// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"
)

// Transcriptions made by an OpenAI-compatible speech-to-text API, without the job service: while a transcription
// runs, each participant's client cuts its own microphone's audio into utterances and sends them here; they are
// transcribed right away (and shown as live captions), and kept until the transcription stops or the call ends, when
// the transcript is posted in the call's thread like the transcriber's, for the Agents plugin to summarize.

const (
	transcribeAPIOpenAI = "openai"

	// sttJobID marks the transcription jobs run from the clients' audio (CallJob.Props.JobID).
	sttJobID = "stt-api"

	sttSegmentKeyPrefix = "stt_"
	sttMaxAudioBytes    = 8 * 1024 * 1024
	sttRequestTimeout   = 2 * time.Minute

	// An utterance is placed on the server's clock by how long before it was sent it started, by the client's
	// clock, capped at this (a client's clock can jump).
	sttMaxUtteranceAge = 10 * time.Minute
)

// sttGracePeriod is how long after a transcription stops the clients' last utterances are still taken (each
// client sends what was said up to the stop), and so how long its transcript waits.
var sttGracePeriod = 10 * time.Second

type endedSTTJob struct {
	jobID string
	endAt time.Time
}

// endedSTTJobs are the transcriptions stopped in the grace period, by call.
var endedSTTJobs sync.Map

func markSTTJobEnded(callID, jobID string) {
	endedSTTJobs.Store(callID, endedSTTJob{jobID: jobID, endAt: time.Now()})
	time.AfterFunc(sttGracePeriod, func() {
		if v, ok := endedSTTJobs.Load(callID); ok && v.(endedSTTJob).jobID == jobID {
			endedSTTJobs.Delete(callID)
		}
	})
}

func recentlyEndedSTTJob(callID string) string {
	if v, ok := endedSTTJobs.Load(callID); ok && time.Since(v.(endedSTTJob).endAt) < sttGracePeriod {
		return v.(endedSTTJob).jobID
	}
	return ""
}

// sttSegment is an utterance of a participant, transcribed.
type sttSegment struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	// Server time, in milliseconds.
	StartAt int64  `json:"start_at"`
	EndAt   int64  `json:"end_at"`
	Text    string `json:"text"`
}

func isSTTJob(job *public.CallJob) bool {
	return job != nil && job.Props.JobID == sttJobID
}

func isActiveJob(job *public.CallJob) bool {
	return job != nil && job.EndAt == 0
}

func (p *Plugin) publishJobState(callID string, job *public.CallJob, userIDs []string) {
	p.publishWebSocketEvent(wsEventCallJobState, map[string]interface{}{
		"callID":   callID,
		"jobState": getClientStateFromCallJob(job).toMap(),
	}, &WebSocketBroadcast{
		ChannelID:           callID,
		ReliableClusterSend: true,
		UserIDs:             userIDs,
	})
}

// handleTranscriptionAction starts or stops the transcription of a call (host only).
func (p *Plugin) handleTranscriptionAction(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleTranscriptionAction", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]
	action := mux.Vars(r)["action"]

	if !p.API.HasPermissionToChannel(userID, callID, model.PermissionReadChannel) {
		res.Err = "Forbidden"
		res.Code = http.StatusForbidden
		return
	}

	if cfg := p.getConfiguration(); !cfg.transcriptionsEnabled() || !cfg.transcribeWithAPI() {
		res.Err = "Transcriptions by a speech-to-text API are not enabled"
		res.Code = http.StatusForbidden
		return
	}

	state, err := p.lockCallReturnState(callID)
	if err != nil {
		res.Err = fmt.Errorf("failed to lock call: %w", err).Error()
		res.Code = http.StatusInternalServerError
		return
	}
	defer p.unlockCall(callID)

	if state == nil {
		res.Err = "no call ongoing"
		res.Code = http.StatusForbidden
		return
	}
	if state.Call.GetHostID() != userID {
		res.Err = "no permissions to transcribe"
		res.Code = http.StatusForbidden
		return
	}

	var job *public.CallJob
	switch action {
	case "start":
		if isActiveJob(state.Transcription) {
			res.Err = "transcription already in progress"
			res.Code = http.StatusForbidden
			return
		}
		job, err = p.startSTTTranscription(state, callID, userID)
	case "stop":
		if !isActiveJob(state.Transcription) || !isSTTJob(state.Transcription) {
			res.Err = "no transcription in progress"
			res.Code = http.StatusForbidden
			return
		}
		job, err = p.stopSTTTranscription(state, callID)
	default:
		res.Err = "unsupported transcription action"
		res.Code = http.StatusBadRequest
		return
	}
	if err != nil {
		res.Err = err.Error()
		res.Code = http.StatusInternalServerError
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(getClientStateFromCallJob(job)); err != nil {
		p.LogError(err.Error())
	}
}

// startSTTTranscription creates the jobs of a transcription (and of its live captions): they start right away, as the
// clients do the work.
func (p *Plugin) startSTTTranscription(state *callState, callID, userID string) (*public.CallJob, error) {
	now := time.Now().UnixMilli()
	newJob := func(jobType public.JobType) (*public.CallJob, error) {
		job := &public.CallJob{
			ID:        model.NewId(),
			CallID:    state.Call.ID,
			Type:      jobType,
			CreatorID: userID,
			InitAt:    now,
			StartAt:   now,
			Props:     public.CallJobProps{JobID: sttJobID},
		}
		if err := p.store.CreateCallJob(job); err != nil {
			return nil, fmt.Errorf("failed to create call job: %w", err)
		}
		return job, nil
	}

	trJob, err := newJob(public.JobTypeTranscribing)
	if err != nil {
		return nil, err
	}
	userIDs := getUserIDsFromSessions(state.sessions)
	p.publishJobState(callID, trJob, userIDs)

	if p.getConfiguration().liveCaptionsEnabled() {
		lcJob, err := newJob(public.JobTypeCaptioning)
		if err != nil {
			return nil, err
		}
		p.publishJobState(callID, lcJob, userIDs)
	}

	return trJob, nil
}

// stopSTTTranscription ends the jobs of a transcription and posts its transcript.
func (p *Plugin) stopSTTTranscription(state *callState, callID string) (*public.CallJob, error) {
	now := time.Now().UnixMilli()
	userIDs := getUserIDsFromSessions(state.sessions)
	for _, job := range []*public.CallJob{state.LiveCaptions, state.Transcription} {
		if !isActiveJob(job) || !isSTTJob(job) {
			continue
		}
		job.EndAt = now
		if err := p.store.UpdateCallJob(job); err != nil {
			return nil, fmt.Errorf("failed to update call job: %w", err)
		}
		p.publishJobState(callID, job, userIDs)
	}

	call := state.Call
	job := state.Transcription
	markSTTJobEnded(callID, job.ID)
	go p.postSTTTranscript(&call, job)
	return job, nil
}

// handleTranscriptionAudio transcribes an utterance of a participant of a call being transcribed.
//
// The body is multipart: the audio file ("audio", 16 kHz WAV from our clients, or any format the API takes), the
// participant's call session ("session_id"), and when the utterance started and when it was sent, by the client's
// clock in milliseconds ("start_at", "sent_at"), from which the server places it on its own clock.
func (p *Plugin) handleTranscriptionAudio(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleTranscriptionAudio", &res, w, r)

	received := time.Now()
	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	cfg := p.getConfiguration()
	if !cfg.transcriptionsEnabled() || !cfg.transcribeWithAPI() {
		res.Err = "Transcriptions by a speech-to-text API are not enabled"
		res.Code = http.StatusForbidden
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, sttMaxAudioBytes+64*1024)
	if err := r.ParseMultipartForm(sttMaxAudioBytes); err != nil {
		res.Err = "failed to parse the audio: " + err.Error()
		res.Code = http.StatusBadRequest
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	sessionID := r.FormValue("session_id")
	startAt, errStart := strconv.ParseFloat(r.FormValue("start_at"), 64)
	sentAt, errSent := strconv.ParseFloat(r.FormValue("sent_at"), 64)
	if sessionID == "" || errStart != nil || errSent != nil || sentAt < startAt {
		res.Err = "session_id, start_at and sent_at are required"
		res.Code = http.StatusBadRequest
		return
	}

	state, err := p.getCallState(callID, false)
	if err != nil {
		res.Err = fmt.Errorf("failed to get call state: %w", err).Error()
		res.Code = http.StatusInternalServerError
		return
	}
	jobID, live := "", false
	if state != nil && isActiveJob(state.Transcription) && isSTTJob(state.Transcription) {
		jobID, live = state.Transcription.ID, isActiveJob(state.LiveCaptions)
	} else if state != nil {
		jobID = recentlyEndedSTTJob(callID)
	}
	if jobID == "" {
		res.Err = "no transcription in progress"
		res.Code = http.StatusConflict
		return
	}
	session := state.sessions[sessionID]
	if session == nil || session.UserID != userID {
		res.Err = "not in the call"
		res.Code = http.StatusForbidden
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		res.Err = "the audio is missing: " + err.Error()
		res.Code = http.StatusBadRequest
		return
	}
	defer file.Close()

	text, err := p.transcribeAudio(r.Context(), cfg, file, header.Filename, header.Header.Get("Content-Type"))
	if err != nil {
		p.LogError("failed to transcribe audio", "callID", callID, "err", err.Error())
		res.Err = "failed to transcribe the audio"
		res.Code = http.StatusBadGateway
		return
	}

	if text != "" {
		duration := time.Duration((sentAt - startAt) * float64(time.Millisecond))
		if duration > sttMaxUtteranceAge {
			duration = sttMaxUtteranceAge
		}
		segment := sttSegment{
			UserID:    userID,
			SessionID: sessionID,
			StartAt:   received.Add(-duration).UnixMilli(),
			EndAt:     received.UnixMilli(),
			Text:      text,
		}
		if err := p.saveSTTSegment(jobID, segment); err != nil {
			p.LogError("failed to save transcript segment", "callID", callID, "err", err.Error())
		}

		if live {
			p.publishWebSocketEvent(wsEventCaption, map[string]interface{}{
				"channel_id": callID,
				"user_id":    userID,
				"session_id": sessionID,
				"text":       text,
			}, &WebSocketBroadcast{
				ChannelID:           callID,
				ReliableClusterSend: true,
				UserIDs:             getUserIDsFromSessions(state.sessions),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"text": text}); err != nil {
		p.LogError(err.Error())
	}
}

// transcribeAudio sends audio to the speech-to-text API and returns its text.
func (p *Plugin) transcribeAudio(ctx context.Context, cfg *configuration, audio io.Reader, filename, contentType string) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if filename == "" {
		filename = "audio.webm"
	}
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename=%q`, filename)},
		"Content-Type":        {orDefault(contentType, "application/octet-stream")},
	})
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, audio); err != nil {
		return "", err
	}
	fields := map[string]string{"model": cfg.TranscribeAPIOpenAIModel, "response_format": "json"}
	if cfg.LiveCaptionsLanguage != "" {
		fields["language"] = cfg.LiveCaptionsLanguage
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return "", err
		}
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, sttRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.TranscribeAPIOpenAIURL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if cfg.TranscribeAPIOpenAIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.TranscribeAPIOpenAIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the speech-to-text API returned status %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("invalid response from the speech-to-text API: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func sttSegmentPrefix(jobID string) string {
	return sttSegmentKeyPrefix + jobID + "_"
}

func (p *Plugin) saveSTTSegment(jobID string, segment sttSegment) error {
	data, err := json.Marshal(segment)
	if err != nil {
		return err
	}
	// The start time first, so the keys sort in the transcript's order.
	key := fmt.Sprintf("%s%013d_%s", sttSegmentPrefix(jobID), segment.StartAt, segment.SessionID)
	p.metrics.IncStoreOp("KVSet")
	if appErr := p.API.KVSet(key, data); appErr != nil {
		return appErr
	}
	return nil
}

// loadSTTSegments returns the segments of a transcription, in order, and their keys.
func (p *Plugin) loadSTTSegments(jobID string) ([]sttSegment, []string, error) {
	prefix := sttSegmentPrefix(jobID)
	var keys []string
	for page := 0; ; page++ {
		p.metrics.IncStoreOp("KVList")
		pageKeys, appErr := p.API.KVList(page, 1000)
		if appErr != nil {
			return nil, nil, appErr
		}
		for _, key := range pageKeys {
			if strings.HasPrefix(key, prefix) {
				keys = append(keys, key)
			}
		}
		if len(pageKeys) < 1000 {
			break
		}
	}
	sort.Strings(keys)

	segments := make([]sttSegment, 0, len(keys))
	for _, key := range keys {
		p.metrics.IncStoreOp("KVGet")
		data, appErr := p.API.KVGet(key)
		if appErr != nil || data == nil {
			continue
		}
		var segment sttSegment
		if err := json.Unmarshal(data, &segment); err == nil {
			segments = append(segments, segment)
		}
	}
	sort.SliceStable(segments, func(i, j int) bool { return segments[i].StartAt < segments[j].StartAt })
	return segments, keys, nil
}

func formatVTTTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ms := d.Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// buildTranscript returns the WebVTT captions and the plain text of a transcript. Each cue names its speaker both as
// a voice span and in the text, as the job service's transcriber does, since readers of the captions (e.g. the
// Agents plugin) keep only the text.
func buildTranscript(segments []sttSegment, startAt int64, names map[string]string) ([]byte, []byte) {
	var vtt, txt bytes.Buffer
	vtt.WriteString("WEBVTT\n")
	lastSpeaker := ""
	for i, s := range segments {
		name := names[s.UserID]
		start := time.Duration(s.StartAt-startAt) * time.Millisecond
		end := time.Duration(s.EndAt-startAt) * time.Millisecond
		fmt.Fprintf(&vtt, "\n%d\n%s --> %s\n<v %s>(%s) %s\n", i+1, formatVTTTime(start), formatVTTTime(end), name, name, s.Text)

		if s.UserID != lastSpeaker {
			if txt.Len() > 0 {
				txt.WriteString("\n\n")
			}
			fmt.Fprintf(&txt, "%s (%s)\n", name, formatVTTTime(start)[:8])
			lastSpeaker = s.UserID
		} else {
			txt.WriteString(" ")
		}
		txt.WriteString(s.Text)
	}
	txt.WriteString("\n")
	return vtt.Bytes(), txt.Bytes()
}

// postSTTTranscript posts the transcript of a transcription in the call's thread (in the channel when the call has
// no post), and asks for its summary.
func (p *Plugin) postSTTTranscript(call *public.Call, job *public.CallJob) {
	if job == nil {
		return
	}
	time.Sleep(sttGracePeriod)
	segments, keys, err := p.loadSTTSegments(job.ID)
	if err != nil {
		p.LogError("failed to load transcript", "trID", job.ID, "err", err.Error())
		return
	}
	defer func() {
		for _, key := range keys {
			p.metrics.IncStoreOp("KVDelete")
			if appErr := p.API.KVDelete(key); appErr != nil {
				p.LogError("failed to delete transcript segment", "key", key, "err", appErr.Error())
			}
		}
	}()
	if len(segments) == 0 {
		return
	}

	names := map[string]string{}
	for _, s := range segments {
		if _, ok := names[s.UserID]; ok {
			continue
		}
		names[s.UserID] = s.UserID
		if user, appErr := p.API.GetUser(s.UserID); appErr == nil {
			names[s.UserID] = user.GetDisplayName(model.ShowFullName)
		}
	}
	vtt, txt := buildTranscript(segments, job.StartAt, names)

	name := "transcript-" + time.UnixMilli(job.StartAt).UTC().Format("2006-01-02-1504")
	vttInfo, appErr := p.API.UploadFile(vtt, call.ChannelID, name+".vtt")
	if appErr != nil {
		p.LogError("failed to upload transcript", "trID", job.ID, "err", appErr.Error())
		return
	}
	txtInfo, appErr := p.API.UploadFile(txt, call.ChannelID, name+".txt")
	if appErr != nil {
		p.LogError("failed to upload transcript", "trID", job.ID, "err", appErr.Error())
		return
	}

	T := p.getTranslationFunc("")
	msg := T("app.call.new_transcription_message")
	if call.Title != "" {
		msg = fmt.Sprintf("%s of %s at %s UTC", msg, call.Title, time.UnixMilli(job.StartAt).UTC().Format("3:04PM"))
	}
	language := p.getConfiguration().LiveCaptionsLanguage
	post := &model.Post{
		UserId:    p.getBotID(),
		ChannelId: call.ChannelID,
		Message:   msg,
		Type:      callTranscriptionType,
		FileIds:   []string{txtInfo.Id},
	}
	post.AddProp("transcription_id", job.ID)
	post.AddProp("captions", public.Transcriptions{{Language: language, FileIDs: []string{vttInfo.Id, txtInfo.Id}}}.ToClientCaptions())

	var callPost *model.Post
	if call.PostID != "" {
		if callPost, err = p.store.GetPost(call.PostID); err != nil {
			p.LogError("failed to get call post", "trID", job.ID, "err", err.Error())
			callPost = nil
		}
	}
	if callPost != nil {
		post.RootId = callPost.Id
		if callPost.RootId != "" {
			post.RootId = callPost.RootId
		}
		post.AddProp("call_post_id", callPost.Id)

		// The captions are readable as files of the call post, without showing there.
		if err := p.store.UpdateFileInfoPostID(vttInfo.Id, call.ChannelID, callPost.Id); err != nil {
			p.LogError("failed to attach captions to call post", "trID", job.ID, "err", err.Error())
		}
	}

	trPost, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.LogError("failed to post transcript", "trID", job.ID, "err", appErr.Error())
		return
	}

	if callPost == nil {
		// Without a call post, the captions are attached to the transcription post.
		if err := p.store.UpdateFileInfoPostID(vttInfo.Id, call.ChannelID, trPost.Id); err != nil {
			p.LogError("failed to attach captions", "trID", job.ID, "err", err.Error())
		}
	} else if err := p.addTranscriptionToCallPost(call.ChannelID, callPost.Id, job.ID, vttInfo.Id, trPost.Id); err != nil {
		p.LogError("failed to update call post", "trID", job.ID, "err", err.Error())
	}

	if p.getConfiguration().summarizeTranscriptions() {
		p.requestTranscriptionSummary(trPost.Id, job.CreatorID)
	}
}

// addTranscriptionToCallPost records a transcription in the call post's props, as the job service's are.
func (p *Plugin) addTranscriptionToCallPost(channelID, callPostID, jobID, fileID, postID string) error {
	if _, err := p.lockCallReturnState(channelID); err != nil {
		return err
	}
	defer p.unlockCall(channelID)

	callPost, err := p.store.GetPost(callPostID)
	if err != nil {
		return err
	}
	transcriptions, _ := callPost.GetProp("transcriptions").(map[string]any)
	if transcriptions == nil {
		transcriptions = map[string]any{}
	}
	tm := jobMetadata{FileID: fileID, PostID: postID}
	transcriptions[jobID] = tm.toMap()
	callPost.AddProp("transcriptions", transcriptions)
	if _, appErr := p.API.UpdatePost(callPost); appErr != nil {
		return appErr
	}
	return nil
}

// requestTranscriptionSummary asks the Agents plugin to summarize a transcription in its thread, for who started it.
func (p *Plugin) requestTranscriptionSummary(postID, userID string) {
	body, _ := json.Marshal(map[string]string{"post_id": postID, "user_id": userID})
	req, err := http.NewRequest(http.MethodPost, "/mattermost-ai/bridge/v1/meetings/summarize_transcription", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp := p.API.PluginHTTP(req)
	if resp == nil {
		p.LogError("failed to request transcription summary: the Agents plugin didn't answer")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		p.LogError("failed to request transcription summary", "status", resp.StatusCode, "err", string(data))
	}
}
