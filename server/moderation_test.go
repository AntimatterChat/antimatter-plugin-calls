// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
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
)

// fakeKV backs the KV calls of the mocked plugin API with a map, with the atomic semantics of the server.
type fakeKV struct {
	mut  sync.Mutex
	data map[string][]byte
}

func newFakeKV(mockAPI *pluginMocks.MockAPI) *fakeKV {
	kv := &fakeKV{data: map[string][]byte{}}
	mockAPI.On("KVGet", mock.Anything).Return(func(key string) ([]byte, *model.AppError) {
		kv.mut.Lock()
		defer kv.mut.Unlock()
		return kv.data[key], nil
	})
	mockAPI.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(func(key string, value []byte, opts model.PluginKVSetOptions) (bool, *model.AppError) {
		kv.mut.Lock()
		defer kv.mut.Unlock()
		if opts.Atomic {
			old := opts.OldValue
			if !bytes.Equal(old, kv.data[key]) || (old == nil && kv.data[key] != nil) {
				return false, nil
			}
		}
		if value == nil {
			delete(kv.data, key)
		} else {
			kv.data[key] = value
		}
		return true, nil
	})
	mockAPI.On("KVCompareAndDelete", mock.Anything, mock.Anything).Return(func(key string, old []byte) (bool, *model.AppError) {
		kv.mut.Lock()
		defer kv.mut.Unlock()
		if !bytes.Equal(old, kv.data[key]) {
			return false, nil
		}
		delete(kv.data, key)
		return true, nil
	})
	return kv
}

func (kv *fakeKV) mutes(t *testing.T, channelID string) modMutes {
	t.Helper()
	kv.mut.Lock()
	defer kv.mut.Unlock()
	data := kv.data[modMuteKey(channelID)]
	if data == nil {
		return nil
	}
	var mutes modMutes
	require.NoError(t, json.Unmarshal(data, &mutes))
	return mutes
}

func (kv *fakeKV) setMutes(t *testing.T, channelID string, mutes modMutes) {
	t.Helper()
	data, err := json.Marshal(mutes)
	require.NoError(t, err)
	kv.mut.Lock()
	defer kv.mut.Unlock()
	kv.data[modMuteKey(channelID)] = data
}

func TestModMuteLifecycle(t *testing.T) {
	mockAPI := &pluginMocks.MockAPI{}
	mockMetrics := &serverMocks.MockMetrics{}
	p := Plugin{
		MattermostPlugin: plugin.MattermostPlugin{API: mockAPI},
		metrics:          mockMetrics,
		nodeID:           "nodeA",
	}
	kv := newFakeKV(mockAPI)
	mockAPI.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything)
	mockAPI.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil)
	mockLogs(mockAPI)
	mockMetrics.On("IncWebSocketEvent", mock.Anything, mock.Anything)
	mockMetrics.On("IncClusterEvent", mock.Anything)

	resendDelays := modMuteResendDelays
	modMuteResendDelays = nil
	t.Cleanup(func() { modMuteResendDelays = resendDelays })

	channelID := model.NewId()
	userID := model.NewId()
	modID := model.NewId()

	state := &callState{
		Call:     public.Call{ID: model.NewId(), ChannelID: channelID, Props: public.CallProps{NodeID: "nodeB"}},
		sessions: map[string]*public.CallSession{},
	}

	t.Run("muting binds the mute to the call", func(t *testing.T) {
		_, err := p.updateModMutes(channelID, func(mutes modMutes, now int64) bool {
			mutes[userID] = modMute{By: modID, At: now}
			return true
		})
		require.NoError(t, err)
		require.Equal(t, int64(0), kv.mutes(t, channelID)[userID].Until)
		require.True(t, p.isModMuted(channelID, userID))
		require.Equal(t, map[string]string{userID: modID}, p.clientModMuted(channelID))
	})

	t.Run("leaving starts the grace period, which keeps the mute", func(t *testing.T) {
		// Still in the call with another session: nothing changes.
		state.sessions["s2"] = &public.CallSession{ID: "s2", UserID: userID}
		p.onLeaveModMuted(state, channelID, userID)
		require.Equal(t, int64(0), kv.mutes(t, channelID)[userID].Until)

		delete(state.sessions, "s2")
		before := time.Now().UnixMilli()
		p.onLeaveModMuted(state, channelID, userID)
		until := kv.mutes(t, channelID)[userID].Until
		require.GreaterOrEqual(t, until, before+modMuteGrace.Milliseconds())
		require.LessOrEqual(t, until, time.Now().UnixMilli()+modMuteGrace.Milliseconds())
		require.True(t, p.isModMuted(channelID, userID))
	})

	t.Run("joining again within the grace period binds it again", func(t *testing.T) {
		state.sessions["s3"] = &public.CallSession{ID: "s3", UserID: userID}
		p.onJoinModMuted(state, channelID, userID, "s3", "nodeB")
		require.Equal(t, int64(0), kv.mutes(t, channelID)[userID].Until)

		// The new session is muted in rtcd (through the handler node) and everyone is told.
		mockAPI.AssertCalled(t, "PublishPluginClusterEvent", mock.MatchedBy(func(ev model.PluginClusterEvent) bool {
			return ev.Id == string(clusterMessageTypeUserState) && strings.Contains(string(ev.Data), `"type":"mute"`)
		}), mock.Anything)
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventModMute, map[string]interface{}{
			"channel_id": channelID,
			"user_id":    userID,
			"muted":      true,
			"by":         modID,
		}, mock.Anything)
	})

	t.Run("the call ending starts the grace period of everyone", func(t *testing.T) {
		p.releaseModMutes(channelID, "")
		require.NotZero(t, kv.mutes(t, channelID)[userID].Until)
	})

	t.Run("an expired mute is dropped", func(t *testing.T) {
		kv.setMutes(t, channelID, modMutes{userID: {By: modID, At: 1, Until: time.Now().Add(-time.Second).UnixMilli()}})
		p.modMuteCache = modMuteCache{}
		require.False(t, p.isModMuted(channelID, userID))

		other := model.NewId()
		p.onJoinModMuted(state, channelID, other, "s4", "nodeB")
		_, muted, err := p.rebindModMute(channelID, userID)
		require.NoError(t, err)
		require.False(t, muted)

		// Nothing left: the key is gone.
		kv.mut.Lock()
		_, ok := kv.data[modMuteKey(channelID)]
		kv.mut.Unlock()
		require.False(t, ok)
	})
}

func TestModerationRank(t *testing.T) {
	teamID := model.NewId()
	channelID := model.NewId()
	voiceID := model.NewId()
	dmID := model.NewId()

	const (
		sysadmin   = "sysadmin"
		teamAdmin  = "teamadmin"
		chanAdmin  = "chanadmin"
		moderator  = "moderator"
		host       = "host"
		regular    = "regular"
		nonsession = ""
	)

	mockAPI := &pluginMocks.MockAPI{}
	p := Plugin{MattermostPlugin: plugin.MattermostPlugin{API: mockAPI}}
	p.channelSettings.entries = map[string]channelSettingsCacheEntry{
		voiceID: {
			channel:   &public.CallsChannel{ChannelID: voiceID, Props: map[string]any{public.ChannelPropBroadcastSessionState: true}},
			expiresAt: time.Now().Add(time.Hour),
		},
		channelID: {expiresAt: time.Now().Add(time.Hour)},
	}

	channels := map[string]*model.Channel{
		channelID: {Id: channelID, TeamId: teamID, Type: model.ChannelTypeOpen},
		voiceID:   {Id: voiceID, TeamId: teamID, Type: model.ChannelTypeOpen},
		dmID:      {Id: dmID, Type: model.ChannelTypeDirect},
	}
	for id, ch := range channels {
		mockAPI.On("GetChannel", id).Return(ch, nil)
	}
	mockAPI.On("HasPermissionTo", sysadmin, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", mock.Anything, model.PermissionManageSystem).Return(false)
	mockAPI.On("HasPermissionToTeam", teamAdmin, teamID, model.PermissionManageTeam).Return(true)
	mockAPI.On("HasPermissionToChannel", teamAdmin, channelID, model.PermissionManageChannelRoles).Return(true)
	mockAPI.On("HasPermissionToChannel", teamAdmin, voiceID, model.PermissionManageChannelRoles).Return(true)
	mockAPI.On("HasPermissionToChannel", chanAdmin, channelID, model.PermissionManageChannelRoles).Return(true)
	mockAPI.On("HasPermissionToChannel", chanAdmin, voiceID, model.PermissionManageChannelRoles).Return(true)
	mockAPI.On("HasPermissionToTeam", moderator, teamID, model.PermissionRemoveUserFromTeam).Return(true)
	mockAPI.On("HasPermissionToTeam", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockAPI.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(false)

	state := &callState{Call: public.Call{Props: public.CallProps{Hosts: []string{host}}}}

	tcs := []struct {
		user                 string
		channel, voice, inDM int
	}{
		{sysadmin, modRankSystemAdmin, modRankSystemAdmin, modRankSystemAdmin},
		{teamAdmin, modRankTeamAdmin, modRankTeamAdmin, modRankNone},
		{chanAdmin, modRankChannelAdmin, modRankChannelAdmin, modRankNone},
		{moderator, modRankNone, modRankVoiceModerator, modRankNone},
		{host, modRankNone, modRankNone, modRankNone},
		{regular, modRankNone, modRankNone, modRankNone},
		{nonsession, modRankNone, modRankNone, modRankNone},
	}
	for _, tc := range tcs {
		t.Run(tc.user, func(t *testing.T) {
			require.Equal(t, tc.channel, p.moderationRank(tc.user, channels[channelID]))
			require.Equal(t, tc.voice, p.moderationRank(tc.user, channels[voiceID]))
			require.Equal(t, tc.inDM, p.moderationRank(tc.user, channels[dmID]))

			// The host may moderate their call, community moderators the voice channels only.
			isHost := tc.user == host
			require.Equal(t, tc.channel > modRankNone || isHost, p.canModerateCall(tc.user, channelID, state))
			require.Equal(t, tc.voice > modRankNone || isHost, p.canModerateCall(tc.user, voiceID, state))
		})
	}

	t.Run("hierarchy of the moderator mute", func(t *testing.T) {
		require.NoError(t, p.checkCanModMute(sysadmin, teamAdmin, channelID, state))
		require.NoError(t, p.checkCanModMute(chanAdmin, regular, channelID, state))
		require.NoError(t, p.checkCanModMute(host, regular, channelID, state))
		require.NoError(t, p.checkCanModMute(moderator, regular, voiceID, state))
		require.ErrorIs(t, p.checkCanModMute(host, chanAdmin, channelID, state), ErrOutranked)
		require.ErrorIs(t, p.checkCanModMute(chanAdmin, teamAdmin, channelID, state), ErrOutranked)
		require.ErrorIs(t, p.checkCanModMute(moderator, regular, channelID, state), ErrNoPermissions)
		require.ErrorIs(t, p.checkCanModMute(regular, host, channelID, state), ErrNoPermissions)
		require.ErrorIs(t, p.checkCanModMute(sysadmin, sysadmin, channelID, state), ErrNotAllowed)
	})
}

func TestModerationErrorCodes(t *testing.T) {
	mockAPI := &pluginMocks.MockAPI{}
	p := Plugin{MattermostPlugin: plugin.MattermostPlugin{API: mockAPI}}
	mockLogs(mockAPI)

	for err, code := range map[error]int{
		ErrNoPermissions:   http.StatusForbidden,
		ErrOutranked:       http.StatusForbidden,
		ErrRateLimited:     http.StatusTooManyRequests,
		ErrNotInCall:       http.StatusBadRequest,
		ErrNoCallOngoing:   http.StatusBadRequest,
		ErrNotAllowed:      http.StatusBadRequest,
		errors.New("boom"): http.StatusInternalServerError,
	} {
		var res httpResponse
		p.handleModerationError(err, &res, "test")
		require.Equal(t, code, res.Code, err.Error())
	}
}

// TestModerationControls runs the moderation controls against a real call state.
func TestModerationControls(t *testing.T) {
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

	resendDelays := modMuteResendDelays
	modMuteResendDelays = nil
	t.Cleanup(func() { modMuteResendDelays = resendDelays })

	// The handler of the calls' media is another node: muting goes through cluster messages.
	const handlerID = "nodeB"

	// The moderator mutes are written through the API but read from the store: write them to its table.
	writeKV := func(key string, value []byte) {
		_, err := store.WriterDB().Exec(`DELETE FROM pluginkeyvaluestore WHERE pluginid = $1 AND pkey = $2`, manifest.Id, key)
		require.NoError(t, err)
		if value != nil {
			_, err = store.WriterDB().Exec(`INSERT INTO pluginkeyvaluestore (pluginid, pkey, pvalue, expireat) VALUES ($1, $2, $3, 0)`, manifest.Id, key, value)
			require.NoError(t, err)
		}
	}
	isModMuteKey := func(key string) bool { return strings.HasPrefix(key, modMuteKeyPrefix) }
	mockAPI.On("KVSetWithOptions", mock.MatchedBy(isModMuteKey), mock.Anything, mock.Anything).Return(func(key string, value []byte, _ model.PluginKVSetOptions) (bool, *model.AppError) {
		writeKV(key, value)
		return true, nil
	})
	mockAPI.On("KVCompareAndDelete", mock.MatchedBy(isModMuteKey), mock.Anything).Return(func(key string, _ []byte) (bool, *model.AppError) {
		writeKV(key, nil)
		return true, nil
	})

	askAllowed := true
	mockAPI.On("KVSetWithOptions", mock.MatchedBy(func(key string) bool { return strings.HasPrefix(key, askUnmutePrefix) }), mock.Anything, mock.Anything).
		Return(func(string, []byte, model.PluginKVSetOptions) (bool, *model.AppError) {
			ok := askAllowed
			askAllowed = false
			return ok, nil
		})

	mockLogs(mockAPI)
	mockAPI.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockAPI.On("KVDelete", mock.Anything).Return(nil)
	mockAPI.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything)
	mockAPI.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil)

	mockMetrics.On("IncWebSocketEvent", mock.AnythingOfType("string"), mock.AnythingOfType("string"))
	mockMetrics.On("IncClusterEvent", mock.AnythingOfType("string"))
	mockMetrics.On("ObserveClusterMutexGrabTime", "mutex_call", mock.AnythingOfType("float64"))
	mockMetrics.On("ObserveClusterMutexLockedTime", "mutex_call", mock.AnythingOfType("float64"))
	mockMetrics.On("ObserveAppHandlersTime", mock.AnythingOfType("string"), mock.AnythingOfType("float64"))

	hostID := model.NewId()
	adminID := model.NewId()

	mockAPI.On("HasPermissionTo", adminID, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", mock.Anything, model.PermissionManageSystem).Return(false)
	mockAPI.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(false)

	setupCall := func(t *testing.T) (channelID, callID string, users, sessions []string) {
		t.Helper()
		channelID = model.NewId()
		callID = model.NewId()
		mockAPI.On("GetChannel", channelID).Return(&model.Channel{Id: channelID, Type: model.ChannelTypeGroup}, nil)

		require.NoError(t, p.store.CreateCall(&public.Call{
			ID:        callID,
			CreateAt:  time.Now().UnixMilli(),
			ChannelID: channelID,
			StartAt:   time.Now().UnixMilli(),
			OwnerID:   hostID,
			Props:     public.CallProps{NodeID: handlerID, Hosts: []string{hostID}},
		}))
		users = []string{hostID, model.NewId(), model.NewId()}
		for _, userID := range users {
			sessionID := model.NewId()
			require.NoError(t, p.store.CreateCallSession(&public.CallSession{
				ID:      sessionID,
				CallID:  callID,
				UserID:  userID,
				JoinAt:  time.Now().UnixMilli(),
				Unmuted: true,
			}))
			sessions = append(sessions, sessionID)
		}
		return
	}

	unmutedInDB := func(t *testing.T, sessionID string) bool {
		t.Helper()
		s, err := p.store.GetCallSession(sessionID, db.GetCallSessionOpts{FromWriter: true})
		require.NoError(t, err)
		return s.Unmuted
	}

	t.Run("a moderator mute mutes and keeps from unmuting", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		channelID, callID, users, sessions := setupCall(t)
		target := users[1]

		require.NoError(t, p.setModMuted(hostID, channelID, target, true))
		require.False(t, unmutedInDB(t, sessions[1]))
		require.True(t, unmutedInDB(t, sessions[2]))
		mutes, _, err := p.readModMutes(channelID)
		require.NoError(t, err)
		require.Equal(t, hostID, mutes[target].By)
		require.Zero(t, mutes[target].Until)
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventHostMute, map[string]interface{}{
			"channel_id": channelID,
			"session_id": sessions[1],
		}, mock.Anything)

		// Unmuting is refused...
		us := &session{userID: target, channelID: channelID, connID: sessions[1], originalConnID: sessions[1], callID: callID}
		require.NoError(t, p.handleClientMsg(us, clientMessage{Type: clientMessageTypeUnmute}, handlerID))
		require.False(t, unmutedInDB(t, sessions[1]))

		// ...but not for the others.
		other := &session{userID: users[2], channelID: channelID, connID: sessions[2], originalConnID: sessions[2], callID: callID}
		require.NoError(t, p.handleClientMsg(other, clientMessage{Type: clientMessageTypeMute}, handlerID))
		require.NoError(t, p.handleClientMsg(other, clientMessage{Type: clientMessageTypeUnmute}, handlerID))
		require.True(t, unmutedInDB(t, sessions[2]))

		// The call state tells clients.
		state, err := p.getCallState(channelID, true)
		require.NoError(t, err)
		require.Equal(t, map[string]string{target: hostID}, p.getClientStateFor(state, target).ModMuted)

		// Lifting it lets them unmute again.
		require.NoError(t, p.setModMuted(adminID, channelID, target, false))
		p.modMuteCache = modMuteCache{}
		require.NoError(t, p.handleClientMsg(us, clientMessage{Type: clientMessageTypeUnmute}, handlerID))
		require.True(t, unmutedInDB(t, sessions[1]))
	})

	t.Run("mod-mute checks", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		channelID, _, users, _ := setupCall(t)

		require.ErrorIs(t, p.setModMuted(hostID, channelID, model.NewId(), true), ErrNotInCall)
		require.ErrorIs(t, p.setModMuted(hostID, channelID, hostID, true), ErrNotAllowed)
		require.ErrorIs(t, p.setModMuted(users[1], channelID, users[2], true), ErrNoPermissions)
		require.ErrorIs(t, p.setModMuted(hostID, model.NewId(), users[1], true), ErrNoCallOngoing)

		// Someone muted by an admin can't be let go by the host.
		require.NoError(t, p.setModMuted(adminID, channelID, users[1], true))
		require.ErrorIs(t, p.setModMuted(hostID, channelID, users[1], false), ErrOutranked)
	})

	t.Run("ask to unmute", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		channelID, callID, users, sessions := setupCall(t)
		askAllowed = true

		// Only muted people can be asked.
		require.ErrorIs(t, p.askToUnmute(hostID, channelID, sessions[1]), ErrNotAllowed)
		us := &session{userID: users[1], channelID: channelID, connID: sessions[1], originalConnID: sessions[1], callID: callID}
		require.NoError(t, p.handleClientMsg(us, clientMessage{Type: clientMessageTypeMute}, handlerID))

		require.ErrorIs(t, p.askToUnmute(users[2], channelID, sessions[1]), ErrNoPermissions)
		require.ErrorIs(t, p.askToUnmute(hostID, channelID, sessions[0]), ErrNotAllowed)
		require.ErrorIs(t, p.askToUnmute(hostID, channelID, model.NewId()), ErrNotInCall)

		require.NoError(t, p.askToUnmute(hostID, channelID, sessions[1]))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventAskUnmute, map[string]interface{}{
			"channel_id": channelID,
			"session_id": sessions[1],
			"by":         hostID,
		}, &model.WebsocketBroadcast{UserId: users[1], ReliableClusterSend: true})

		// Asking again too soon is refused.
		require.ErrorIs(t, p.askToUnmute(hostID, channelID, sessions[1]), ErrRateLimited)

		// Someone moderator-muted can't unmute: no point asking.
		askAllowed = true
		require.NoError(t, p.setModMuted(hostID, channelID, users[1], true))
		require.ErrorIs(t, p.askToUnmute(hostID, channelID, sessions[1]), ErrNotAllowed)
	})
}
