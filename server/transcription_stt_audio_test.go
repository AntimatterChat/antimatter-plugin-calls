// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/antimatterchat/antimatter-plugin-calls/server/cluster"
	"github.com/antimatterchat/antimatter-plugin-calls/server/public"

	serverMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/antimatterchat/antimatter-plugin-calls/server/interfaces"
	pluginMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/mattermost/mattermost/server/public/plugin"
)

// TestTranscriptionAudioMutes is a regression test for the speech-to-text audio endpoint: the client sends nothing
// while muted only because a muted track is silent, so the server must itself refuse audio from someone a moderator
// muted or in a stage's audience (whose words would otherwise reach every participant as captions and the channel
// as the transcript), and audio sent for someone else's session.
func TestTranscriptionAudioMutes(t *testing.T) {
	mockAPI := &pluginMocks.MockAPI{}
	mockMetrics := &serverMocks.MockMetrics{}

	p := Plugin{
		MattermostPlugin:  plugin.MattermostPlugin{API: mockAPI},
		callsClusterLocks: map[string]*cluster.Mutex{},
		metrics:           mockMetrics,
		nodeID:            "nodeA",
		sessions:          map[string]*session{},
	}

	store, tearDown := NewTestStore(t)
	t.Cleanup(tearDown)
	p.store = store

	// The speech-to-text API counts the utterances it's sent.
	var transcribed atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		transcribed.Add(1)
		_, _ = w.Write([]byte(`{"text":"Hello there."}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &configuration{}
	cfg.SetDefaults()
	cfg.EnableTranscriptions = model.NewPointer(true)
	cfg.TranscribeAPI = transcribeAPIOpenAI
	cfg.TranscribeAPIOpenAIURL = srv.URL + "/v1"
	cfg.TranscribeAPIOpenAIModel = "whisper-1"
	p.configuration = cfg

	mockLogs(mockAPI)
	mockAPI.On("KVSet", mock.Anything, mock.Anything).Return(nil)
	mockAPI.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()
	mockAPI.On("HasPermissionTo", mock.Anything, mock.Anything).Return(false)
	mockAPI.On("HasPermissionToTeam", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockAPI.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockMetrics.On("IncStoreOp", mock.AnythingOfType("string")).Maybe()
	mockMetrics.On("ObserveAppHandlersTime", mock.AnythingOfType("string"), mock.AnythingOfType("float64")).Maybe()

	// The moderator mutes and the stage speakers are read from the store's KV table.
	writeKV := func(t *testing.T, key string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		require.NoError(t, err)
		_, err = store.WriterDB().Exec(`INSERT INTO pluginkeyvaluestore (pluginid, pkey, pvalue, expireat) VALUES ($1, $2, $3, 0)`, manifest.Id, key, data)
		require.NoError(t, err)
	}

	teamID := model.NewId()
	channels := map[string]*model.Channel{}
	mockAPI.On("GetChannel", mock.Anything).Return(func(id string) (*model.Channel, *model.AppError) {
		ch, ok := channels[id]
		if !ok {
			return nil, model.NewAppError("GetChannel", "not_found", nil, "", http.StatusNotFound)
		}
		return ch.DeepCopy(), nil
	})

	// newCall starts a call being transcribed in a new channel (a stage of a voice channel if asked), with a session
	// for each user, and returns the channel and the sessions by user.
	newCall := func(t *testing.T, stage bool, users ...string) (string, map[string]string) {
		t.Helper()
		channelID := model.NewId()
		channels[channelID] = &model.Channel{Id: channelID, TeamId: teamID, Type: model.ChannelTypeOpen}
		if stage {
			channels[channelID].Props = map[string]any{channelPropMode: channelModeStage}
			p.channelSettings.mut.Lock()
			if p.channelSettings.entries == nil {
				p.channelSettings.entries = map[string]channelSettingsCacheEntry{}
			}
			p.channelSettings.entries[channelID] = channelSettingsCacheEntry{
				channel:   &public.CallsChannel{ChannelID: channelID, Enabled: true, Props: map[string]any{public.ChannelPropBroadcastSessionState: true}},
				expiresAt: time.Now().Add(time.Hour),
			}
			p.channelSettings.mut.Unlock()
		}

		now := time.Now().UnixMilli()
		callID := model.NewId()
		require.NoError(t, store.CreateCall(&public.Call{
			ID:        callID,
			CreateAt:  now,
			ChannelID: channelID,
			StartAt:   now,
			OwnerID:   users[0],
			Props:     public.CallProps{NodeID: p.nodeID, Hosts: []string{users[0]}},
		}))
		sessions := map[string]string{}
		for _, userID := range users {
			sessionID := model.NewId()
			require.NoError(t, store.CreateCallSession(&public.CallSession{
				ID:      sessionID,
				CallID:  callID,
				UserID:  userID,
				JoinAt:  now,
				Unmuted: true,
			}))
			sessions[userID] = sessionID
		}
		require.NoError(t, store.CreateCallJob(&public.CallJob{
			ID:        model.NewId(),
			CallID:    callID,
			Type:      public.JobTypeTranscribing,
			CreatorID: users[0],
			InitAt:    now,
			StartAt:   now,
			Props:     public.CallJobProps{JobID: sttJobID},
		}))
		return channelID, sessions
	}

	// send posts an utterance as userID for sessionID, returning the response's status.
	send := func(t *testing.T, userID, channelID, sessionID string) int {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		now := time.Now().UnixMilli()
		require.NoError(t, mw.WriteField("session_id", sessionID))
		require.NoError(t, mw.WriteField("start_at", strconv.FormatInt(now-1500, 10)))
		require.NoError(t, mw.WriteField("sent_at", strconv.FormatInt(now, 10)))
		fw, err := mw.CreateFormFile("audio", "utterance.wav")
		require.NoError(t, err)
		_, err = fw.Write([]byte("RIFF"))
		require.NoError(t, err)
		require.NoError(t, mw.Close())

		r := httptest.NewRequest(http.MethodPost, "/calls/"+channelID+"/transcription/audio", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.Header.Set("Mattermost-User-Id", userID)
		r = mux.SetURLVars(r, map[string]string{"call_id": channelID})
		w := httptest.NewRecorder()
		p.handleTranscriptionAudio(w, r)
		return w.Code
	}

	host, member, other := model.NewId(), model.NewId(), model.NewId()

	t.Run("a participant who may speak is transcribed", func(t *testing.T) {
		defer ResetTestStore(t, store)
		channelID, sessions := newCall(t, false, host, member)
		before := transcribed.Load()
		require.Equal(t, http.StatusOK, send(t, member, channelID, sessions[member]))
		require.Equal(t, before+1, transcribed.Load())
	})

	t.Run("audio for someone else's session is refused", func(t *testing.T) {
		defer ResetTestStore(t, store)
		channelID, sessions := newCall(t, false, host, member)
		before := transcribed.Load()
		require.Equal(t, http.StatusForbidden, send(t, other, channelID, sessions[member]))
		require.Equal(t, http.StatusForbidden, send(t, other, channelID, model.NewId()))
		require.Equal(t, before, transcribed.Load())
	})

	t.Run("someone a moderator muted is refused", func(t *testing.T) {
		defer ResetTestStore(t, store)
		channelID, sessions := newCall(t, false, host, member)
		writeKV(t, modMuteKey(channelID), modMutes{member: {By: host, At: time.Now().UnixMilli()}})
		before := transcribed.Load()
		require.Equal(t, http.StatusForbidden, send(t, member, channelID, sessions[member]))
		require.Equal(t, before, transcribed.Load())

		// The others still are transcribed.
		require.Equal(t, http.StatusOK, send(t, host, channelID, sessions[host]))
	})

	t.Run("someone a moderator deafened is refused", func(t *testing.T) {
		defer ResetTestStore(t, store)
		channelID, sessions := newCall(t, false, host, member)
		writeKV(t, modMuteKey(channelID), modMutes{member: {DeafBy: host, DeafAt: time.Now().UnixMilli()}})
		before := transcribed.Load()
		require.Equal(t, http.StatusForbidden, send(t, member, channelID, sessions[member]))
		require.Equal(t, before, transcribed.Load())
	})

	t.Run("a stage's audience is refused, its invited speakers aren't", func(t *testing.T) {
		defer ResetTestStore(t, store)
		speaker := model.NewId()
		channelID, sessions := newCall(t, true, host, member, speaker)
		writeKV(t, stageSpeakersKey(channelID), stageSpeakers{speaker: {Since: time.Now().UnixMilli()}})
		before := transcribed.Load()
		require.Equal(t, http.StatusForbidden, send(t, member, channelID, sessions[member]))
		require.Equal(t, before, transcribed.Load())
		require.Equal(t, http.StatusOK, send(t, speaker, channelID, sessions[speaker]))
		require.Equal(t, before+1, transcribed.Load())
	})
}
