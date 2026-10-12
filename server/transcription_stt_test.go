// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"
)

func TestBuildTranscript(t *testing.T) {
	start := int64(1_000_000)
	segments := []sttSegment{
		{UserID: "u1", StartAt: start + 1500, EndAt: start + 4000, Text: "Good morning everyone."},
		{UserID: "u1", StartAt: start + 4200, EndAt: start + 6000, Text: "Let's start."},
		{UserID: "u2", StartAt: start + 3_661_250, EndAt: start + 3_663_000, Text: "Sounds good."},
	}
	vtt, txt := buildTranscript(segments, start, map[string]string{"u1": "Maya Chen", "u2": "Arjun"})

	require.Equal(t, `WEBVTT

1
00:00:01.500 --> 00:00:04.000
<v Maya Chen>(Maya Chen) Good morning everyone.

2
00:00:04.200 --> 00:00:06.000
<v Maya Chen>(Maya Chen) Let's start.

3
01:01:01.250 --> 01:01:03.000
<v Arjun>(Arjun) Sounds good.
`, string(vtt))
	require.Equal(t, "Maya Chen (00:00:01)\nGood morning everyone. Let's start.\n\nArjun (01:01:01)\nSounds good.\n", string(txt))
}

func TestTranscribeAudio(t *testing.T) {
	var got struct {
		path, auth, model, language, filename, contentType, audio string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		require.NoError(t, r.ParseMultipartForm(1<<20))
		got.model = r.FormValue("model")
		got.language = r.FormValue("language")
		f, h, err := r.FormFile("file")
		require.NoError(t, err)
		data, _ := io.ReadAll(f)
		got.audio, got.filename, got.contentType = string(data), h.Filename, h.Header.Get("Content-Type")
		if got.model == "broken" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"out of memory"}`))
			return
		}
		_, _ = w.Write([]byte(`{"text":"  Hello there. "}`))
	}))
	defer srv.Close()

	p := &Plugin{}
	cfg := &configuration{TranscribeAPIOpenAIURL: srv.URL + "/v1/", TranscribeAPIOpenAIModel: "parakeet", TranscribeAPIOpenAIKey: "k"}
	cfg.LiveCaptionsLanguage = "en"

	text, err := p.transcribeAudio(context.Background(), cfg, strings.NewReader("OPUS"), "a.webm", "audio/webm")
	require.NoError(t, err)
	require.Equal(t, "Hello there.", text)
	require.Equal(t, "/v1/audio/transcriptions", got.path)
	require.Equal(t, "Bearer k", got.auth)
	require.Equal(t, "parakeet", got.model)
	require.Equal(t, "en", got.language)
	require.Equal(t, "a.webm", got.filename)
	require.Equal(t, "audio/webm", got.contentType)
	require.Equal(t, "OPUS", got.audio)

	cfg.TranscribeAPIOpenAIModel = "broken"
	cfg.TranscribeAPIOpenAIKey = ""
	_, err = p.transcribeAudio(context.Background(), cfg, strings.NewReader("OPUS"), "", "")
	require.ErrorContains(t, err, "status 500")
	require.Empty(t, got.auth)
	require.Equal(t, "audio.webm", got.filename)
}

func TestTranscribeWithAPIConfig(t *testing.T) {
	cfg := &configuration{}
	cfg.SetDefaults()
	cfg.EnableTranscriptions = model.NewPointer(true)
	cfg.EnableLiveCaptions = model.NewPointer(true)

	// Without recordings, the job service's transcriptions are off...
	require.False(t, cfg.transcriptionsEnabled())

	// ...but the API's need no recordings.
	cfg.TranscribeAPI = transcribeAPIOpenAI
	require.True(t, cfg.transcriptionsEnabled())
	require.True(t, cfg.liveCaptionsEnabled())
	require.ErrorContains(t, cfg.IsValid(), "TranscribeAPIOpenAIURL")

	cfg.TranscribeAPIOpenAIURL = "http://stt.example.com/v1"
	require.ErrorContains(t, cfg.IsValid(), "TranscribeAPIOpenAIModel")

	cfg.TranscribeAPIOpenAIModel = "whisper-1"
	require.NoError(t, cfg.IsValid())

	p := &Plugin{}
	client := p.getClientConfig(cfg)
	require.True(t, client.TranscribeFromClients)
	require.True(t, *client.EnableTranscriptions)
	require.True(t, *client.EnableLiveCaptions)

	// Recordings don't start the job service's transcriber then.
	require.False(t, cfg.transcribeWithJobService())
	cfg.TranscribeAPI = "whisper"
	cfg.EnableRecordings = model.NewPointer(true)
	require.True(t, cfg.transcribeWithJobService())
}

func TestStopTranscribingJobLeavesSTTJobs(t *testing.T) {
	// Stopping a recording stops its transcription, but not one by a speech-to-text API, which isn't tied to it (and
	// has no job to stop: the plugin has neither store nor job service here).
	p := &Plugin{}
	job := &public.CallJob{ID: model.NewId(), Type: public.JobTypeTranscribing, StartAt: 1, Props: public.CallJobProps{JobID: sttJobID}}
	state := &callState{Call: public.Call{ID: model.NewId()}, Transcription: job}
	require.NoError(t, p.stopTranscribingJob(state, model.NewId()))
	require.Zero(t, job.EndAt)
}

func TestFormatVTTTime(t *testing.T) {
	require.Equal(t, "00:00:00.000", formatVTTTime(-time.Second))
	require.Equal(t, "10:02:03.004", formatVTTTime(10*time.Hour+2*time.Minute+3*time.Second+4*time.Millisecond))
}

func TestRecentlyEndedSTTJob(t *testing.T) {
	defer func(d time.Duration) { sttGracePeriod = d }(sttGracePeriod)
	sttGracePeriod = 50 * time.Millisecond

	require.Empty(t, recentlyEndedSTTJob("call1"))
	markSTTJobEnded("call1", "job1")
	require.Equal(t, "job1", recentlyEndedSTTJob("call1"))
	require.Empty(t, recentlyEndedSTTJob("call2"))

	time.Sleep(80 * time.Millisecond)
	require.Empty(t, recentlyEndedSTTJob("call1"))
}
