// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/cluster"
	"github.com/antimatterchat/antimatter-plugin-calls/server/db"
	"github.com/antimatterchat/antimatter-plugin-calls/server/public"

	serverMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/antimatterchat/antimatter-plugin-calls/server/interfaces"
	pluginMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/mattermost/mattermost/server/public/plugin"
	rtcMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/mattermost/rtcd/service/rtc"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/rtcd/service/rtc"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// quietT lets mock assertions be polled: it records failures instead of failing the test.
type quietT struct{ failed bool }

func (q *quietT) Logf(string, ...interface{})   {}
func (q *quietT) Errorf(string, ...interface{}) { q.failed = true }
func (q *quietT) FailNow()                      { q.failed = true }

// TestVoiceModeration runs the moderator deafen, the lasting mutes of voice channels and moving people between voice
// channels against a real call state.
func TestVoiceModeration(t *testing.T) {
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

	mockLogs(mockAPI)

	// An RTC server that isn't started: messages to it are queued, and the calls' media are handled by another node,
	// so that muting, deafening and disconnecting go through cluster messages.
	rtcServer, err := rtc.NewServer(rtc.ServerConfig{ICEPortUDP: 33443, ICEPortTCP: 33443, UDPSocketsCount: 1}, newLogger(&p), &rtcMocks.MockMetrics{})
	require.NoError(t, err)
	p.rtcServer = rtcServer
	const handlerID = "nodeB"

	resendDelays := modMuteResendDelays
	modMuteResendDelays = nil
	t.Cleanup(func() { modMuteResendDelays = resendDelays })
	moveDelay := moveDisconnectDelay
	moveDisconnectDelay = 0
	t.Cleanup(func() { moveDisconnectDelay = moveDelay })

	// The moderator mutes are written through the API but read from the store: write them to its table, and keep
	// how long they were set for.
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
	isModMuteKey := func(key string) bool { return strings.HasPrefix(key, modMuteKeyPrefix) }
	mockAPI.On("KVSetWithOptions", mock.MatchedBy(isModMuteKey), mock.Anything, mock.Anything).Return(func(key string, value []byte, opts model.PluginKVSetOptions) (bool, *model.AppError) {
		writeKV(key, value)
		ttlMut.Lock()
		ttls[key] = opts.ExpireInSeconds
		ttlMut.Unlock()
		return true, nil
	})
	mockAPI.On("KVCompareAndDelete", mock.MatchedBy(isModMuteKey), mock.Anything).Return(func(key string, _ []byte) (bool, *model.AppError) {
		writeKV(key, nil)
		return true, nil
	})
	ttlOf := func(channelID string) int64 {
		ttlMut.Lock()
		defer ttlMut.Unlock()
		return ttls[modMuteKey(channelID)]
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

	teamID := model.NewId()
	otherTeamID := model.NewId()
	hostID := model.NewId()
	modID := model.NewId()
	adminID := model.NewId()
	users := []string{hostID, model.NewId(), model.NewId()}
	unknownID := model.NewId()

	// The community's moderators moderate its voice channels; users[1] may join the channels moved to, users[2] not.
	mockAPI.On("HasPermissionTo", adminID, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", mock.Anything, model.PermissionManageSystem).Return(false)
	mockAPI.On("HasPermissionToTeam", modID, teamID, model.PermissionRemoveUserFromTeam).Return(true)
	mockAPI.On("HasPermissionToTeam", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockAPI.On("HasPermissionToChannel", users[1], mock.Anything, model.PermissionCreatePost).Return(true)
	mockAPI.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockAPI.On("GetChannel", unknownID).Return(nil, model.NewAppError("GetChannel", "not_found", nil, "", 404))

	type channelSet struct{ from, to, text, elsewhere, archived, closed string }
	setCallsChannel := func(cc *public.CallsChannel) {
		p.channelSettings.mut.Lock()
		defer p.channelSettings.mut.Unlock()
		if p.channelSettings.entries == nil {
			p.channelSettings.entries = map[string]channelSettingsCacheEntry{}
		}
		p.channelSettings.entries[cc.ChannelID] = channelSettingsCacheEntry{channel: cc, expiresAt: time.Now().Add(time.Hour)}
	}
	newChannels := func() channelSet {
		c := channelSet{from: model.NewId(), to: model.NewId(), text: model.NewId(), elsewhere: model.NewId(), archived: model.NewId(), closed: model.NewId()}
		for id, ch := range map[string]*model.Channel{
			c.from:      {Id: c.from, TeamId: teamID, Type: model.ChannelTypeOpen},
			c.to:        {Id: c.to, TeamId: teamID, Type: model.ChannelTypeOpen},
			c.text:      {Id: c.text, TeamId: teamID, Type: model.ChannelTypeOpen},
			c.elsewhere: {Id: c.elsewhere, TeamId: otherTeamID, Type: model.ChannelTypeOpen},
			c.archived:  {Id: c.archived, TeamId: teamID, Type: model.ChannelTypeOpen, DeleteAt: 1},
			c.closed:    {Id: c.closed, TeamId: teamID, Type: model.ChannelTypeOpen},
		} {
			mockAPI.On("GetChannel", id).Return(ch, nil)
			var props map[string]any
			if id != c.text {
				props = map[string]any{public.ChannelPropBroadcastSessionState: true}
			}
			// The closed one is a voice channel with calls turned off.
			setCallsChannel(&public.CallsChannel{ChannelID: id, Enabled: id != c.closed, Props: props})
		}
		return c
	}

	setupCall := func(t *testing.T, channelID string) []string {
		t.Helper()
		callID := model.NewId()
		require.NoError(t, p.store.CreateCall(&public.Call{
			ID:        callID,
			CreateAt:  time.Now().UnixMilli(),
			ChannelID: channelID,
			StartAt:   time.Now().UnixMilli(),
			OwnerID:   hostID,
			Props:     public.CallProps{NodeID: handlerID, Hosts: []string{hostID}},
		}))
		var sessions []string
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
		return sessions
	}

	unmutedInDB := func(t *testing.T, sessionID string) bool {
		t.Helper()
		s, err := p.store.GetCallSession(sessionID, db.GetCallSessionOpts{FromWriter: true})
		require.NoError(t, err)
		return s.Unmuted
	}

	// sentUserState tells whether the handler node was told to apply a user state message to a session.
	sentUserState := func(sessionID, msgType string) bool {
		return mockAPI.AssertCalled(&quietT{}, "PublishPluginClusterEvent", mock.MatchedBy(func(ev model.PluginClusterEvent) bool {
			data := string(ev.Data)
			return ev.Id == string(clusterMessageTypeUserState) && strings.Contains(data, `"conn_id":"`+sessionID+`"`) &&
				strings.Contains(data, `"type":"`+msgType+`"`)
		}), mock.Anything)
	}

	modMutesOf := func(t *testing.T, channelID string) modMutes {
		t.Helper()
		mutes, _, err := p.readModMutes(channelID)
		require.NoError(t, err)
		return mutes
	}

	t.Run("a moderator deafen deafens and mutes, until lifted", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		c := newChannels()
		sessions := setupCall(t, c.from)
		target := users[1]

		require.NoError(t, p.setModDeafened(modID, c.from, target, true))
		require.False(t, unmutedInDB(t, sessions[1]))
		require.True(t, unmutedInDB(t, sessions[2]))
		m := modMutesOf(t, c.from)[target]
		require.Equal(t, modID, m.DeafBy)
		require.Empty(t, m.By)
		require.Zero(t, m.Until)
		require.True(t, p.isModDeafened(c.from, target))
		require.True(t, p.isModMuted(c.from, target))

		// rtcd deafens and mutes the session (on the node handling the call's media), and everyone is told.
		require.True(t, sentUserState(sessions[1], clusterUserStateDeafen))
		require.True(t, sentUserState(sessions[1], clientMessageTypeMute))
		require.False(t, sentUserState(sessions[2], clusterUserStateDeafen))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventModDeafen, map[string]interface{}{
			"channel_id": c.from,
			"user_id":    target,
			"deafened":   true,
			"by":         modID,
		}, mock.Anything)

		// Unmuting is refused.
		callID := func() string {
			state, err := p.getCallState(c.from, true)
			require.NoError(t, err)
			return state.Call.ID
		}()
		us := &session{userID: target, channelID: c.from, connID: sessions[1], originalConnID: sessions[1], callID: callID}
		require.NoError(t, p.handleClientMsg(us, clientMessage{Type: clientMessageTypeUnmute}, handlerID))
		require.False(t, unmutedInDB(t, sessions[1]))

		// Clients see who is deafened; a deafen alone isn't a moderator mute.
		state, err := p.getCallState(c.from, true)
		require.NoError(t, err)
		require.Equal(t, map[string]string{target: modID}, p.getClientStateFor(state, target).ModDeafened)
		require.Nil(t, p.getClientStateFor(state, target).ModMuted)

		// The node handling the session keeps it deafened.
		p.enforceModDeafenOf(us)
		require.True(t, us.rtcDeafened.Load())

		// The host ranks below the moderator who deafened them: they can't lift it.
		require.ErrorIs(t, p.setModDeafened(hostID, c.from, target, false), ErrOutranked)

		// Lifting it gives the audio back and lets them unmute.
		require.NoError(t, p.setModDeafened(adminID, c.from, target, false))
		require.True(t, sentUserState(sessions[1], clusterUserStateUndeafen))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventModDeafen, map[string]interface{}{
			"channel_id": c.from,
			"user_id":    target,
			"deafened":   false,
			"by":         adminID,
		}, mock.Anything)
		require.Empty(t, modMutesOf(t, c.from))
		p.modMuteCache = modMuteCache{}
		p.enforceModDeafenOf(us)
		require.False(t, us.rtcDeafened.Load())
		require.NoError(t, p.handleClientMsg(us, clientMessage{Type: clientMessageTypeUnmute}, handlerID))
		require.True(t, unmutedInDB(t, sessions[1]))
	})

	t.Run("a mute and a deafen are lifted apart", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		c := newChannels()
		setupCall(t, c.from)
		target := users[1]

		require.NoError(t, p.setModMuted(modID, c.from, target, true))
		require.NoError(t, p.setModDeafened(modID, c.from, target, true))

		// Undeafened, they stay moderator-muted.
		require.NoError(t, p.setModDeafened(modID, c.from, target, false))
		m := modMutesOf(t, c.from)[target]
		require.Equal(t, modID, m.By)
		require.False(t, m.isDeafened())
		p.modMuteCache = modMuteCache{}
		require.True(t, p.isModMuted(c.from, target))
		require.Equal(t, map[string]string{target: modID}, p.clientModMuted(c.from))
		require.Nil(t, p.clientModDeafened(c.from))

		// Allowed to unmute while deafened, they still can't speak.
		require.NoError(t, p.setModDeafened(modID, c.from, target, true))
		require.NoError(t, p.setModMuted(modID, c.from, target, false))
		m = modMutesOf(t, c.from)[target]
		require.Empty(t, m.By)
		require.Equal(t, modID, m.DeafBy)
		p.modMuteCache = modMuteCache{}
		require.True(t, p.isModMuted(c.from, target))
		require.Nil(t, p.clientModMuted(c.from))

		require.NoError(t, p.setModDeafened(modID, c.from, target, false))
		require.Empty(t, modMutesOf(t, c.from))
	})

	t.Run("deafen checks", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		c := newChannels()
		setupCall(t, c.from)
		setupCall(t, c.text)

		require.ErrorIs(t, p.setModDeafened(modID, c.from, model.NewId(), true), ErrNotInCall)
		require.ErrorIs(t, p.setModDeafened(hostID, c.from, hostID, true), ErrNotAllowed)
		require.ErrorIs(t, p.setModDeafened(users[1], c.from, users[2], true), ErrNoPermissions)
		require.ErrorIs(t, p.setModDeafened(modID, model.NewId(), users[1], true), ErrNoCallOngoing)

		// Deafening is for voice channels.
		require.ErrorIs(t, p.setModDeafened(hostID, c.text, users[1], true), ErrNotAllowed)
		require.ErrorIs(t, p.setModDeafened(adminID, c.text, users[1], true), ErrNotAllowed)

		// Lifting a deafen nobody gave does nothing.
		require.NoError(t, p.setModDeafened(users[2], c.from, users[1], false))
	})

	t.Run("in a voice channel, mutes and deafens outlast the call", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		c := newChannels()
		sessions := setupCall(t, c.from)

		require.NoError(t, p.setModMuted(modID, c.from, users[1], true))
		require.NoError(t, p.setModDeafened(modID, c.from, users[2], true))
		require.Zero(t, ttlOf(c.from))

		// Leaving, and the call ending, start no grace period.
		state, err := p.getCallState(c.from, true)
		require.NoError(t, err)
		delete(state.sessions, sessions[1])
		p.onLeaveModMuted(state, c.from, users[1])
		ResetTestStore(t, p.store)
		p.releaseModMutes(c.from, "")
		for _, userID := range users[1:] {
			require.Zero(t, modMutesOf(t, c.from)[userID].Until)
		}
		p.modMuteCache = modMuteCache{}
		require.True(t, p.isModMuted(c.from, users[1]))
		require.True(t, p.isModDeafened(c.from, users[2]))

		// Joining a later call, they're still muted and deafened.
		sessions = setupCall(t, c.from)
		state, err = p.getCallState(c.from, true)
		require.NoError(t, err)
		p.onJoinModMuted(state, c.from, users[2], sessions[2], handlerID)
		require.True(t, sentUserState(sessions[2], clusterUserStateDeafen))
		require.True(t, sentUserState(sessions[2], clientMessageTypeMute))

		// They can be lifted while the person is away.
		ResetTestStore(t, p.store)
		require.NoError(t, p.setModMuted(modID, c.from, users[1], false))
		require.NoError(t, p.setModDeafened(modID, c.from, users[2], false))
		require.Empty(t, modMutesOf(t, c.from))

		// Elsewhere, a moderator mute lasts the call and a grace period.
		setupCall(t, c.text)
		require.NoError(t, p.setModMuted(hostID, c.text, users[1], true))
		require.Equal(t, int64(modMuteKeyTTL.Seconds()), ttlOf(c.text))
		ResetTestStore(t, p.store)
		p.releaseModMutes(c.text, "")
		require.NotZero(t, modMutesOf(t, c.text)[users[1]].Until)
	})

	t.Run("moving someone to another voice channel", func(t *testing.T) {
		defer ResetTestStore(t, p.store)
		c := newChannels()
		sessions := setupCall(t, c.from)
		textSessions := setupCall(t, c.text)

		require.NoError(t, p.moveSession(modID, c.from, sessions[1], c.to))
		mockAPI.AssertCalled(t, "PublishWebSocketEvent", wsEventHostMove, map[string]interface{}{
			"channel_id":    c.from,
			"session_id":    sessions[1],
			"to_channel_id": c.to,
			"by":            modID,
		}, &model.WebsocketBroadcast{UserId: users[1], ReliableClusterSend: true})

		// Still in the first call once the delay is over, they're disconnected from it.
		require.Eventually(t, func() bool {
			return mockAPI.AssertCalled(&quietT{}, "PublishPluginClusterEvent", mock.MatchedBy(func(ev model.PluginClusterEvent) bool {
				return ev.Id == string(clusterMessageTypeDisconnect) && strings.Contains(string(ev.Data), `"conn_id":"`+sessions[1]+`"`)
			}), mock.Anything)
		}, 5*time.Second, 10*time.Millisecond)

		// Those who may moderate both channels move people below them.
		require.NoError(t, p.moveSession(adminID, c.from, sessions[1], c.to))
		require.ErrorIs(t, p.moveSession(users[2], c.from, sessions[1], c.to), ErrNoPermissions)

		// The host of the first call isn't a moderator of the second.
		require.ErrorIs(t, p.moveSession(hostID, c.from, sessions[1], c.to), ErrNoPermissions)

		// Only between the open voice channels of the community, which the person may join.
		require.ErrorIs(t, p.moveSession(modID, c.from, sessions[1], c.from), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(adminID, c.from, sessions[1], c.text), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(adminID, c.from, sessions[1], c.elsewhere), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(adminID, c.from, sessions[1], c.archived), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(adminID, c.from, sessions[1], unknownID), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(adminID, c.from, sessions[1], c.closed), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(adminID, c.text, textSessions[1], c.to), ErrNotAllowed)
		require.ErrorIs(t, p.moveSession(modID, c.from, sessions[2], c.to), ErrNotAllowed)

		require.ErrorIs(t, p.moveSession(modID, c.from, model.NewId(), c.to), ErrNotInCall)
		require.ErrorIs(t, p.moveSession(modID, model.NewId(), sessions[1], c.to), ErrNoCallOngoing)
	})
}
