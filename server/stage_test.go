// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/cluster"
	"github.com/antimatterchat/antimatter-plugin-calls/server/db"
	"github.com/antimatterchat/antimatter-plugin-calls/server/public"

	serverMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/antimatterchat/antimatter-plugin-calls/server/interfaces"
	pluginMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/mattermost/mattermost/server/public/plugin"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// TestStage runs stages (CHANNEL_TYPES_DESIGN.md §3.5) against a real call state: who speaks, who invites and moves
// whom, the host, switching a channel to a stage, and how long the speakers are kept.
func TestStage(t *testing.T) {
	mockAPI := &pluginMocks.MockAPI{}
	mockMetrics := &serverMocks.MockMetrics{}

	p := Plugin{
		MattermostPlugin:  plugin.MattermostPlugin{API: mockAPI},
		callsClusterLocks: map[string]*cluster.Mutex{},
		metrics:           mockMetrics,
		nodeID:            "nodeA",
		sessions:          map[string]*session{},
		apiLimiters:       map[string]*rate.Limiter{},
	}

	store, tearDown := NewTestStore(t)
	t.Cleanup(tearDown)
	p.store = store

	mockLogs(mockAPI)

	resendDelays := modMuteResendDelays
	modMuteResendDelays = nil
	t.Cleanup(func() { modMuteResendDelays = resendDelays })

	// The calls' media are handled by another node: muting goes through cluster messages.
	const handlerID = "nodeB"

	// The moderator mutes and the speakers are written through the API but read from the store: write them to its
	// table, and keep how long they were set for.
	var ttlMut sync.Mutex
	ttls := map[string]int64{}
	writeKV := func(key string, value []byte) {
		_, err := store.WriterDB().Exec(`DELETE FROM pluginkeyvaluestore WHERE pluginid = $1 AND pkey = $2`, manifest.Id, key)
		require.NoError(t, err)
		if value != nil {
			_, err = store.WriterDB().Exec(`INSERT INTO pluginkeyvaluestore (pluginid, pkey, pvalue, expireat) VALUES ($1, $2, $3, 0)`, manifest.Id, key, value)
			require.NoError(t, err)
		}
	}
	isStoredKey := func(key string) bool {
		return strings.HasPrefix(key, modMuteKeyPrefix) || strings.HasPrefix(key, stageSpeakersKeyPrefix)
	}
	mockAPI.On("KVSetWithOptions", mock.MatchedBy(isStoredKey), mock.Anything, mock.Anything).Return(func(key string, value []byte, opts model.PluginKVSetOptions) (bool, *model.AppError) {
		writeKV(key, value)
		ttlMut.Lock()
		ttls[key] = opts.ExpireInSeconds
		ttlMut.Unlock()
		return true, nil
	})
	mockAPI.On("KVCompareAndDelete", mock.MatchedBy(isStoredKey), mock.Anything).Return(func(key string, _ []byte) (bool, *model.AppError) {
		writeKV(key, nil)
		return true, nil
	})
	ttlOf := func(channelID string) int64 {
		ttlMut.Lock()
		defer ttlMut.Unlock()
		return ttls[stageSpeakersKey(channelID)]
	}

	mockAPI.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockAPI.On("KVDelete", mock.Anything).Return(nil)
	mockAPI.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything)
	mockAPI.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil)

	mockMetrics.On("IncWebSocketEvent", mock.AnythingOfType("string"), mock.AnythingOfType("string"))
	mockMetrics.On("IncClusterEvent", mock.AnythingOfType("string"))
	mockMetrics.On("ObserveClusterMutexGrabTime", "mutex_call", mock.AnythingOfType("float64"))
	mockMetrics.On("ObserveClusterMutexLockedTime", "mutex_call", mock.AnythingOfType("float64"))
	mockMetrics.On("ObserveAppHandlersTime", mock.AnythingOfType("string"), mock.AnythingOfType("float64"))
	mockMetrics.On("Handler").Return(nil)

	teamID := model.NewId()
	modID := model.NewId()
	mod2ID := model.NewId()
	teamAdminID := model.NewId()
	adminID := model.NewId()
	a, b, c := model.NewId(), model.NewId(), model.NewId()

	// The community's moderators moderate its voice channels: they speak on its stages, as do its admins.
	mockAPI.On("HasPermissionTo", adminID, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", mock.Anything, model.PermissionManageSystem).Return(false)
	mockAPI.On("HasPermissionToTeam", teamAdminID, teamID, model.PermissionManageTeam).Return(true)
	mockAPI.On("HasPermissionToTeam", modID, teamID, model.PermissionRemoveUserFromTeam).Return(true)
	mockAPI.On("HasPermissionToTeam", mod2ID, teamID, model.PermissionDeleteOthersPosts).Return(true)
	mockAPI.On("HasPermissionToTeam", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockAPI.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(false)

	// The voice channels, by ID, with their mode (which can change).
	var chMut sync.Mutex
	channels := map[string]*model.Channel{}
	mockAPI.On("GetChannel", mock.Anything).Return(func(id string) (*model.Channel, *model.AppError) {
		chMut.Lock()
		defer chMut.Unlock()
		ch, ok := channels[id]
		if !ok {
			return nil, model.NewAppError("GetChannel", "not_found", nil, "", http.StatusNotFound)
		}
		return ch.DeepCopy(), nil
	})
	setMode := func(channelID, mode string) {
		chMut.Lock()
		defer chMut.Unlock()
		channels[channelID].Props = map[string]any{channelPropMode: mode}
	}
	newChannel := func(mode string) string {
		id := model.NewId()
		chMut.Lock()
		channels[id] = &model.Channel{Id: id, TeamId: teamID, Type: model.ChannelTypeOpen, Props: map[string]any{channelPropMode: mode}}
		chMut.Unlock()

		// A voice channel: the voice channels app sends its calls' session state to the whole channel.
		p.channelSettings.mut.Lock()
		if p.channelSettings.entries == nil {
			p.channelSettings.entries = map[string]channelSettingsCacheEntry{}
		}
		p.channelSettings.entries[id] = channelSettingsCacheEntry{
			channel:   &public.CallsChannel{ChannelID: id, Enabled: true, Props: map[string]any{public.ChannelPropBroadcastSessionState: true}},
			expiresAt: time.Now().Add(time.Hour),
		}
		p.channelSettings.mut.Unlock()
		return id
	}

	// sessions are the call sessions of each user, by channel.
	sessions := map[string]map[string]string{}
	callState := func(t *testing.T, channelID string) *callState {
		t.Helper()
		state, err := p.getCallState(channelID, true)
		require.NoError(t, err)
		return state
	}
	join := func(t *testing.T, channelID, userID string) string {
		t.Helper()
		state, err := p.lockCallReturnState(channelID)
		require.NoError(t, err)
		defer p.unlockCall(channelID)
		sessionID := model.NewId()
		state, err = p.addUserSession(state, model.NewPointer(true), userID, sessionID, channelID, "")
		require.NoError(t, err)

		// The call's media are on the other node.
		if state.Call.Props.NodeID != handlerID {
			state.Call.Props.NodeID = handlerID
			require.NoError(t, p.store.UpdateCall(&state.Call))
		}
		if sessions[channelID] == nil {
			sessions[channelID] = map[string]string{}
		}
		sessions[channelID][userID] = sessionID
		return sessionID
	}
	leave := func(t *testing.T, channelID, userID string) {
		t.Helper()
		state, err := p.lockCallReturnState(channelID)
		require.NoError(t, err)
		defer p.unlockCall(channelID)
		sessionID := sessions[channelID][userID]
		require.NoError(t, p.removeUserSession(state, userID, sessionID, sessionID, channelID))
		delete(sessions[channelID], userID)
	}
	wsSession := func(t *testing.T, channelID, userID string) *session {
		t.Helper()
		sessionID := sessions[channelID][userID]
		return &session{userID: userID, channelID: channelID, connID: sessionID, originalConnID: sessionID, callID: callState(t, channelID).Call.ID}
	}
	send := func(t *testing.T, channelID, userID, msgType string) error {
		t.Helper()
		return p.handleClientMsg(wsSession(t, channelID, userID), clientMessage{Type: msgType, Data: []byte(`{}`)}, handlerID)
	}
	callSession := func(t *testing.T, channelID, userID string) *public.CallSession {
		t.Helper()
		s, err := p.store.GetCallSession(sessions[channelID][userID], db.GetCallSessionOpts{FromWriter: true})
		require.NoError(t, err)
		return s
	}
	unmuted := func(t *testing.T, channelID, userID string) bool {
		t.Helper()
		return callSession(t, channelID, userID).Unmuted
	}
	hostOf := func(t *testing.T, channelID string) string {
		t.Helper()
		return callState(t, channelID).Call.GetHostID()
	}
	speakersOf := func(t *testing.T, channelID string) stageSpeakers {
		t.Helper()
		speakers, _, err := p.readStageSpeakers(channelID)
		require.NoError(t, err)
		return speakers
	}

	router := p.newAPIRouter()
	serve := func(method, userID, path, body string) int {
		// The API's rate limits aren't what is tested here.
		p.apiLimitersMut.Lock()
		p.apiLimiters = map[string]*rate.Limiter{}
		p.apiLimitersMut.Unlock()

		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if userID != "" {
			r.Header.Set("Mattermost-User-Id", userID)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}
	invite := func(requesterID, channelID, userID string) int {
		return serve(http.MethodPost, requesterID, "/calls/"+channelID+"/stage/speakers", `{"user_id":"`+userID+`"}`)
	}
	toAudience := func(requesterID, channelID, userID string) int {
		return serve(http.MethodDelete, requesterID, "/calls/"+channelID+"/stage/speakers/"+userID, "")
	}

	t.Run("the audience can't unmute, share their screen or turn their camera on; an invited speaker can", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)
		join(t, ch, modID)
		join(t, ch, a)
		join(t, ch, b)

		// Unmuting is refused: rtcd is told the session is muted, and the app to mute itself again.
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))
		require.False(t, unmuted(t, ch, a))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventHostMute, map[string]interface{}{
			"channel_id": ch,
			"session_id": sessions[ch][a],
		}, &model.WebsocketBroadcast{UserId: a, ReliableClusterSend: true})

		// No camera, no screen share (the app is told to stop sharing).
		require.ErrorIs(t, send(t, ch, a, clientMessageTypeVideoOn), ErrNotAllowed)
		require.False(t, callSession(t, ch, a).Video)
		require.ErrorIs(t, send(t, ch, a, clientMessageTypeScreenOn), ErrNotAllowed)
		require.Empty(t, callState(t, ch).Call.Props.ScreenSharingSessionID)
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventHostScreenOff, map[string]interface{}{
			"channel_id": ch,
			"session_id": sessions[ch][a],
		}, &model.WebsocketBroadcast{UserId: a, ReliableClusterSend: true})

		// A moderator speaks by their role.
		require.NoError(t, send(t, ch, modID, clientMessageTypeUnmute))
		require.True(t, unmuted(t, ch, modID))

		// Invited, a can unmute themselves (nobody unmutes them) and turn their camera on; b still can't.
		require.Equal(t, http.StatusOK, invite(modID, ch, a))
		require.False(t, unmuted(t, ch, a))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventStageInvited, map[string]interface{}{
			"channel_id": ch,
			"by":         modID,
		}, &model.WebsocketBroadcast{UserId: a, ReliableClusterSend: true})
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))
		require.True(t, unmuted(t, ch, a))
		require.NoError(t, send(t, ch, a, clientMessageTypeVideoOn))
		require.True(t, callSession(t, ch, a).Video)
		require.NoError(t, send(t, ch, b, clientMessageTypeUnmute))
		require.False(t, unmuted(t, ch, b))

		// Clients learn who speaks.
		st := p.getClientStateFor(callState(t, ch), b)
		require.True(t, st.Stage)
		require.ElementsMatch(t, []string{modID, a}, st.Speakers)
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventStageSpeakersChanged, mock.MatchedBy(func(data map[string]interface{}) bool {
			ids, _ := data["speakers"].([]string)
			return data["channel_id"] == ch && data["stage"] == true && len(ids) == 2
		}), &model.WebsocketBroadcast{ChannelId: ch, ReliableClusterSend: true})
	})

	t.Run("inviting lowers the hand", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)
		join(t, ch, modID)
		join(t, ch, a)

		require.NoError(t, send(t, ch, a, clientMessageTypeRaiseHand))
		require.NotZero(t, callSession(t, ch, a).RaisedHand)
		require.Equal(t, http.StatusOK, invite(modID, ch, a))
		require.Zero(t, callSession(t, ch, a).RaisedHand)
	})

	t.Run("only moderators invite and move, and only people they outrank", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)
		for _, userID := range []string{modID, mod2ID, teamAdminID, a, b, c} {
			join(t, ch, userID)
		}

		// A member may do neither, even to themselves; without a session, nobody is let in.
		require.Equal(t, http.StatusForbidden, invite(b, ch, a))
		require.Equal(t, http.StatusForbidden, invite(a, ch, a))
		require.Equal(t, http.StatusUnauthorized, invite("", ch, a))
		require.NotContains(t, speakersOf(t, ch), a)

		// Nobody invites someone of their own rank or above.
		require.Equal(t, http.StatusForbidden, invite(modID, ch, mod2ID))
		require.Equal(t, http.StatusForbidden, invite(modID, ch, teamAdminID))

		require.Equal(t, http.StatusOK, invite(modID, ch, a))
		require.Equal(t, http.StatusOK, invite(mod2ID, ch, c))
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))

		// A member can't move a speaker to the audience; a moderator can, and that mutes them again.
		require.Equal(t, http.StatusForbidden, toAudience(b, ch, a))
		require.Equal(t, http.StatusForbidden, toAudience(c, ch, a))
		require.Equal(t, http.StatusUnauthorized, toAudience("", ch, a))
		require.Contains(t, speakersOf(t, ch), a)
		require.Equal(t, http.StatusOK, toAudience(mod2ID, ch, a))
		require.NotContains(t, speakersOf(t, ch), a)
		require.False(t, unmuted(t, ch, a))
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))
		require.False(t, unmuted(t, ch, a))

		// Nor does anyone move someone of their own rank or above, and those who speak by their role stay speakers.
		require.Equal(t, http.StatusForbidden, toAudience(modID, ch, mod2ID))
		require.Equal(t, http.StatusForbidden, toAudience(modID, ch, teamAdminID))
		require.Equal(t, http.StatusBadRequest, toAudience(teamAdminID, ch, modID))
		require.Contains(t, speakersOf(t, ch), modID)

		// Outside stages there is nothing to invite to.
		talk := newChannel("talk")
		join(t, talk, modID)
		join(t, talk, a)
		require.Equal(t, http.StatusBadRequest, invite(modID, talk, a))
		require.Equal(t, http.StatusBadRequest, toAudience(modID, talk, a))
	})

	t.Run("a speaker can step down", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)
		join(t, ch, modID)
		join(t, ch, a)
		require.Equal(t, http.StatusOK, invite(modID, ch, a))
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))
		require.True(t, unmuted(t, ch, a))

		require.Equal(t, http.StatusOK, toAudience(a, ch, a))
		require.NotContains(t, speakersOf(t, ch), a)
		require.False(t, unmuted(t, ch, a))

		// A moderator speaks by their role: they can't.
		require.Equal(t, http.StatusBadRequest, toAudience(modID, ch, modID))
	})

	t.Run("the host is never audience", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)

		// With no speaker in the call, there is no host.
		join(t, ch, a)
		join(t, ch, b)
		require.Empty(t, hostOf(t, ch))

		// The first moderator to join becomes it, and leaving takes it away again.
		join(t, ch, modID)
		require.Equal(t, modID, hostOf(t, ch))
		leave(t, ch, modID)
		require.Empty(t, hostOf(t, ch))

		// An invited speaker can be the host, until moved back to the audience.
		join(t, ch, modID)
		require.Equal(t, modID, hostOf(t, ch))
		require.Equal(t, http.StatusOK, invite(modID, ch, a))
		leave(t, ch, modID)
		require.Equal(t, a, hostOf(t, ch))
		join(t, ch, teamAdminID)
		require.Equal(t, a, hostOf(t, ch))
		require.Equal(t, http.StatusOK, toAudience(teamAdminID, ch, a))
		require.Equal(t, teamAdminID, hostOf(t, ch))

		// The host can't be handed to the audience.
		require.ErrorIs(t, p.changeHost(teamAdminID, ch, b), ErrNotAllowed)
		require.Equal(t, teamAdminID, hostOf(t, ch))
		require.NoError(t, p.changeHost(teamAdminID, ch, teamAdminID))

		// Nor do they get the host controls: the audience can't mute anyone.
		require.ErrorIs(t, p.muteSession(b, ch, sessions[ch][teamAdminID]), ErrNoPermissions)
	})

	t.Run("switching talk to stage mutes the non-speakers within the cache period", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel("talk")
		join(t, ch, a)
		join(t, ch, modID)
		join(t, ch, b)
		require.Equal(t, a, hostOf(t, ch))
		for _, userID := range []string{a, b, modID} {
			require.NoError(t, send(t, ch, userID, clientMessageTypeUnmute))
			require.True(t, unmuted(t, ch, userID))
			us := wsSession(t, ch, userID)
			p.mut.Lock()
			p.sessions[us.connID] = us
			p.mut.Unlock()
		}
		t.Cleanup(func() {
			p.mut.Lock()
			p.sessions = map[string]*session{}
			p.mut.Unlock()
		})

		// The node sees a talk call.
		p.enforceModMutes()

		// The channel becomes a stage: within the cache period, the call is unchanged...
		setMode(ch, channelModeStage)
		p.enforceModMutes()
		require.True(t, unmuted(t, ch, a))

		// ...and then the non-speakers are muted, the moderator speaks, and the host is a speaker.
		p.stageCache.setMode(ch, false, time.Now().Add(-2*stageCacheTTL))
		p.enforceModMutes()
		require.False(t, unmuted(t, ch, a))
		require.False(t, unmuted(t, ch, b))
		require.True(t, unmuted(t, ch, modID))
		require.Equal(t, modID, hostOf(t, ch))
		require.Contains(t, speakersOf(t, ch), modID)
		require.NoError(t, send(t, ch, b, clientMessageTypeUnmute))
		require.False(t, unmuted(t, ch, b))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventStageSpeakersChanged, map[string]interface{}{
			"channel_id": ch,
			"stage":      true,
			"speakers":   []string{modID},
		}, &model.WebsocketBroadcast{ChannelId: ch, ReliableClusterSend: true})

		// And back: anyone may speak again.
		setMode(ch, "talk")
		p.stageCache.setMode(ch, true, time.Now().Add(-2*stageCacheTTL))
		p.enforceModMutes()
		require.NoError(t, send(t, ch, b, clientMessageTypeUnmute))
		require.True(t, unmuted(t, ch, b))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventStageSpeakersChanged, map[string]interface{}{
			"channel_id": ch,
			"stage":      false,
			"speakers":   []string{},
		}, &model.WebsocketBroadcast{ChannelId: ch, ReliableClusterSend: true})
	})

	t.Run("speakers survive a rejoin within the grace period, and are gone once the call was empty that long", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)
		join(t, ch, modID)
		join(t, ch, a)
		require.Equal(t, http.StatusOK, invite(modID, ch, a))
		require.Zero(t, speakersOf(t, ch)[a].Until)

		// Leaving starts the grace period, which keeps them a speaker.
		before := time.Now().UnixMilli()
		leave(t, ch, a)
		until := speakersOf(t, ch)[a].Until
		require.GreaterOrEqual(t, until, before+modMuteGrace.Milliseconds())
		require.LessOrEqual(t, until, time.Now().UnixMilli()+modMuteGrace.Milliseconds())
		require.Contains(t, p.getClientStateFor(callState(t, ch), modID).Speakers, a)

		// Rejoining binds them again: they can unmute.
		join(t, ch, a)
		require.Zero(t, speakersOf(t, ch)[a].Until)
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))
		require.True(t, unmuted(t, ch, a))

		// The call ending starts everyone's grace period, and the key expires with it.
		leave(t, ch, a)
		leave(t, ch, modID)
		p.releaseStageSpeakers(ch, "")
		for userID, s := range speakersOf(t, ch) {
			require.NotZero(t, s.Until, userID)
		}
		require.Greater(t, ttlOf(ch), int64(0))
		require.LessOrEqual(t, ttlOf(ch), int64(modMuteGrace.Seconds())+1)

		// Once the call has been empty that long, they're gone, and so is the key.
		expired := stageSpeakers{}
		for userID, s := range speakersOf(t, ch) {
			s.Until = time.Now().Add(-time.Second).UnixMilli()
			expired[userID] = s
		}
		data, err := json.Marshal(expired)
		require.NoError(t, err)
		writeKV(stageSpeakersKey(ch), data)
		speakers, err := p.getStageSpeakers(ch, true)
		require.NoError(t, err)
		require.Empty(t, speakers)
		p.releaseStageSpeakers(ch, "")
		stored, err := p.KVGet(stageSpeakersKey(ch), true)
		require.NoError(t, err)
		require.Nil(t, stored)

		// A speaker who joins a new call after that is in the audience.
		join(t, ch, a)
		require.NoError(t, send(t, ch, a, clientMessageTypeUnmute))
		require.False(t, unmuted(t, ch, a))
	})

	t.Run("the participant menu's stage permissions follow the roles", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		ch := newChannel(channelModeStage)
		for _, userID := range []string{modID, mod2ID, teamAdminID, a, b} {
			join(t, ch, userID)
		}
		require.Equal(t, http.StatusOK, invite(modID, ch, b))

		type flags struct{ invite, toAudience bool }
		flagsOf := func(t *testing.T, channelID, viewer, target string) flags {
			t.Helper()
			perms, err := p.getParticipantPermissions(viewer, channelID, sessions[channelID][target])
			require.NoError(t, err)
			return flags{perms.CanInviteToSpeak, perms.CanMoveToAudience}
		}

		tcs := []struct {
			name   string
			viewer string
			want   map[string]flags
		}{
			{"a moderator invites the audience and moves invited speakers", modID, map[string]flags{
				a: {invite: true}, b: {toAudience: true}, mod2ID: {}, teamAdminID: {}, modID: {},
			}},
			{"a community admin can't move those who speak by their role", teamAdminID, map[string]flags{
				a: {invite: true}, b: {toAudience: true}, modID: {}, mod2ID: {}, teamAdminID: {},
			}},
			{"the audience may do nothing", a, map[string]flags{
				a: {}, b: {}, modID: {},
			}},
			{"an invited speaker may only step down", b, map[string]flags{
				b: {toAudience: true}, a: {}, modID: {},
			}},
		}
		for _, tc := range tcs {
			t.Run(tc.name, func(t *testing.T) {
				for target, want := range tc.want {
					require.Equal(t, want, flagsOf(t, ch, tc.viewer, target), target)
				}
			})
		}

		// Outside stages, there is neither.
		talk := newChannel("talk")
		join(t, talk, modID)
		join(t, talk, a)
		require.Equal(t, flags{}, flagsOf(t, talk, modID, a))
	})
}
