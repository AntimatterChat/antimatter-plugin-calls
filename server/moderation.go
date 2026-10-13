// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/rtcd/service/rtc"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"
)

// Antimatter: moderation of calls, as Discord's voice moderation. The call's host, the channel's and the
// community's admins, the system admins and, in voice channels, the community's moderators may mute someone for
// the call (a moderator mute: they can't unmute until it's lifted), or ask someone muted to unmute.
//
// A moderator mute belongs to the channel: it lasts while the person is in the call, and for modMuteGrace after
// they left it (or the call ended), so that leaving and joining again (even when that restarts the call) doesn't
// clear it. The Calls server enforces it: unmuting is refused and rtcd is told the session is muted, so it doesn't
// forward their voice.

const (
	wsEventModMute    = "mod_mute"
	wsEventAskUnmute  = "ask_unmute"
	modMuteKeyPrefix  = "mod_mutes_"
	askUnmutePrefix   = "ask_unmute_"
	modMuteGrace      = 10 * time.Minute
	modMuteKeyTTL     = 24 * time.Hour
	modMuteCacheTTL   = 5 * time.Second
	askUnmuteInterval = 20 * time.Second

	modMuteUpdateAttempts  = 5
	modMuteEnforceInterval = 2 * time.Second
)

// The re-sends of rtcd's mute message after an unmute was refused: rtcd forwards a voice track created after the
// mute message (it enables new tracks, and drops mute messages for sessions without one), which happens when the
// client adds its track on unmuting.
var modMuteResendDelays = []time.Duration{500 * time.Millisecond, time.Second, 1500 * time.Millisecond}

var (
	ErrRateLimited = errors.New("too many requests")
	ErrOutranked   = errors.New("not allowed to moderate this user")
)

// The moderation ranks, for the moderator mute: someone may only moderator-mute (or lift the mute of) people below
// them. The host rank is only ever the requester's: targets are ranked without it.
const (
	modRankNone = iota
	modRankHost
	modRankVoiceModerator
	modRankChannelAdmin
	modRankTeamAdmin
	modRankSystemAdmin
)

// modMute is someone's moderator mute in a channel.
type modMute struct {
	By string `json:"by"`
	At int64  `json:"at"`

	// When it ends; 0 while the person is in the call.
	Until int64 `json:"until"`
}

func (m modMute) isActive(now int64) bool {
	return m.Until == 0 || now < m.Until
}

// modMutes are the moderator mutes of a channel, by user.
type modMutes map[string]modMute

// prune drops the mutes that ended, telling whether there were any.
func (mm modMutes) prune(now int64) bool {
	pruned := false
	for userID, m := range mm {
		if !m.isActive(now) {
			delete(mm, userID)
			pruned = true
		}
	}
	return pruned
}

func (mm modMutes) clone() modMutes {
	c := make(modMutes, len(mm))
	for k, v := range mm {
		c[k] = v
	}
	return c
}

func modMuteKey(channelID string) string {
	return modMuteKeyPrefix + channelID
}

// modMuteCache keeps the moderator mutes of channels for a few seconds: the unmute path and the enforcer read them.
type modMuteCache struct {
	mut     sync.Mutex
	entries map[string]modMuteCacheEntry
}

type modMuteCacheEntry struct {
	mutes     modMutes
	expiresAt time.Time
}

func (c *modMuteCache) get(channelID string, now time.Time) (modMutes, bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	entry, ok := c.entries[channelID]
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return entry.mutes, true
}

func (c *modMuteCache) set(channelID string, mutes modMutes, now time.Time) {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.entries == nil {
		c.entries = map[string]modMuteCacheEntry{}
	}
	for id, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, id)
		}
	}
	c.entries[channelID] = modMuteCacheEntry{mutes: mutes, expiresAt: now.Add(modMuteCacheTTL)}
}

// readModMutes reads the moderator mutes of a channel as stored, with the stored value for compare-and-set.
func (p *Plugin) readModMutes(channelID string) (modMutes, []byte, error) {
	var data []byte
	if p.store != nil {
		var err error
		if data, err = p.KVGet(modMuteKey(channelID), true); err != nil {
			return nil, nil, fmt.Errorf("failed to get moderator mutes: %w", err)
		}
	} else {
		var appErr *model.AppError
		if data, appErr = p.API.KVGet(modMuteKey(channelID)); appErr != nil {
			return nil, nil, fmt.Errorf("failed to get moderator mutes: %w", appErr)
		}
	}

	mutes := modMutes{}
	if len(data) == 0 {
		return mutes, nil, nil
	}
	if err := json.Unmarshal(data, &mutes); err != nil {
		p.LogWarn("dropping unreadable moderator mutes", "channelID", channelID, "err", err.Error())
		return modMutes{}, data, nil
	}
	return mutes, data, nil
}

// getModMutes gives the moderator mutes in effect in a channel, possibly cached for a few seconds. Don't change it.
func (p *Plugin) getModMutes(channelID string) (modMutes, error) {
	now := time.Now()
	if mutes, ok := p.modMuteCache.get(channelID, now); ok {
		return mutes, nil
	}
	mutes, _, err := p.readModMutes(channelID)
	if err != nil {
		return nil, err
	}
	mutes.prune(now.UnixMilli())
	p.modMuteCache.set(channelID, mutes, now)
	return mutes, nil
}

// isModMuted tells whether someone is moderator-muted in a channel (from the cache).
func (p *Plugin) isModMuted(channelID, userID string) bool {
	mutes, err := p.getModMutes(channelID)
	if err != nil {
		p.LogError("failed to get moderator mutes", "channelID", channelID, "err", err.Error())
		return false
	}
	_, ok := mutes[userID]
	return ok
}

// updateModMutes changes the moderator mutes of a channel atomically; change tells whether it changed anything.
// It returns the mutes in effect afterwards.
func (p *Plugin) updateModMutes(channelID string, change func(mutes modMutes, now int64) bool) (modMutes, error) {
	key := modMuteKey(channelID)
	for i := 0; i < modMuteUpdateAttempts; i++ {
		mutes, old, err := p.readModMutes(channelID)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		pruned := mutes.prune(now.UnixMilli())
		changed := change(mutes, now.UnixMilli())
		if !changed && !pruned {
			p.modMuteCache.set(channelID, mutes.clone(), now)
			return mutes, nil
		}

		var ok bool
		var appErr *model.AppError
		if len(mutes) == 0 && old == nil {
			return mutes, nil
		} else if len(mutes) == 0 {
			ok, appErr = p.API.KVCompareAndDelete(key, old)
		} else {
			data, err := json.Marshal(mutes)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal moderator mutes: %w", err)
			}
			ok, appErr = p.API.KVSetWithOptions(key, data, model.PluginKVSetOptions{
				Atomic:          true,
				OldValue:        old,
				ExpireInSeconds: int64(modMuteKeyTTL.Seconds()),
			})
		}
		if appErr != nil {
			return nil, fmt.Errorf("failed to save moderator mutes: %w", appErr)
		}
		if ok {
			p.modMuteCache.set(channelID, mutes.clone(), now)
			return mutes, nil
		}
		time.Sleep(time.Duration(10*(i+1)) * time.Millisecond)
	}
	return nil, fmt.Errorf("failed to save moderator mutes: too many concurrent changes")
}

// releaseModMutes starts the grace period of the moderator mutes of people who left a channel's call: all of them
// when userID is empty (the call ended).
func (p *Plugin) releaseModMutes(channelID, userID string) {
	if _, err := p.updateModMutes(channelID, func(mutes modMutes, now int64) bool {
		changed := false
		for id, m := range mutes {
			if (userID == "" || id == userID) && m.Until == 0 {
				m.Until = now + modMuteGrace.Milliseconds()
				mutes[id] = m
				changed = true
			}
		}
		return changed
	}); err != nil {
		p.LogError("failed to release moderator mutes", "channelID", channelID, "userID", userID, "err", err.Error())
	}
}

// rebindModMute binds someone's moderator mute to the call again as they join it, telling whether they're
// moderator-muted.
func (p *Plugin) rebindModMute(channelID, userID string) (modMute, bool, error) {
	mutes, err := p.updateModMutes(channelID, func(mutes modMutes, _ int64) bool {
		m, ok := mutes[userID]
		if !ok || m.Until == 0 {
			return false
		}
		m.Until = 0
		mutes[userID] = m
		return true
	})
	if err != nil {
		return modMute{}, false, err
	}
	m, ok := mutes[userID]
	return m, ok, nil
}

// isVoiceRoom tells whether a channel is a voice channel: the voice channels plugin makes its calls' session state
// go to the whole channel.
func (p *Plugin) isVoiceRoom(channelID string) bool {
	return p.getCachedCallsChannel(channelID).BoolProp(public.ChannelPropBroadcastSessionState)
}

// moderationRank is what someone may do in the calls of a channel, whoever hosts them.
func (p *Plugin) moderationRank(userID string, channel *model.Channel) int {
	if userID == "" || channel == nil {
		return modRankNone
	}
	if p.API.HasPermissionTo(userID, model.PermissionManageSystem) {
		return modRankSystemAdmin
	}
	if channel.TeamId != "" && p.API.HasPermissionToTeam(userID, channel.TeamId, model.PermissionManageTeam) {
		return modRankTeamAdmin
	}
	if p.API.HasPermissionToChannel(userID, channel.Id, model.PermissionManageChannelRoles) {
		return modRankChannelAdmin
	}
	if channel.TeamId != "" && p.isVoiceRoom(channel.Id) {
		for _, perm := range []*model.Permission{model.PermissionRemoveUserFromTeam, model.PermissionManageTeamRoles, model.PermissionDeleteOthersPosts} {
			if p.API.HasPermissionToTeam(userID, channel.TeamId, perm) {
				return modRankVoiceModerator
			}
		}
	}
	return modRankNone
}

// requesterRank is moderationRank with the call's host ranked as such.
func (p *Plugin) requesterRank(requesterID string, channel *model.Channel, state *callState) int {
	rank := p.moderationRank(requesterID, channel)
	if rank == modRankNone && state != nil && requesterID != "" && requesterID == state.Call.GetHostID() {
		return modRankHost
	}
	return rank
}

// canModerateCall tells whether someone may use the host controls on the call of a channel.
func (p *Plugin) canModerateCall(requesterID, channelID string, state *callState) bool {
	if state != nil && requesterID != "" && requesterID == state.Call.GetHostID() {
		return true
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.LogError("failed to get channel", "channelID", channelID, "err", appErr.Error())
		return false
	}
	return p.moderationRank(requesterID, channel) > modRankNone
}

// checkCanModMute tells whether a requester may moderator-mute someone (or lift their mute) in a channel.
func (p *Plugin) checkCanModMute(requesterID, targetID, channelID string, state *callState) error {
	if targetID == requesterID || p.isBot(targetID) {
		return ErrNotAllowed
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return fmt.Errorf("failed to get channel: %w", appErr)
	}
	mine := p.requesterRank(requesterID, channel, state)
	if mine == modRankNone {
		return ErrNoPermissions
	}
	if mine <= p.moderationRank(targetID, channel) {
		return ErrOutranked
	}
	return nil
}

// sendSessionMute tells rtcd that a session is muted, through the node handling its call's media.
func (p *Plugin) sendSessionMute(callID, channelID, userID, sessionID, handlerID string) error {
	if p.rtcdManager != nil || handlerID == p.nodeID {
		if p.rtcdManager == nil && p.rtcServer == nil {
			return fmt.Errorf("no RTC server")
		}
		return p.sendRTCMessage(rtc.Message{SessionID: sessionID, Type: rtc.MuteMessage}, callID)
	}
	return p.sendClusterMessage(clusterMessage{
		ConnID:        sessionID,
		UserID:        userID,
		ChannelID:     channelID,
		CallID:        callID,
		SenderID:      p.nodeID,
		ClientMessage: clientMessage{Type: clientMessageTypeMute},
	}, clusterMessageTypeUserState, handlerID)
}

// enforceModMute mutes a moderator-muted session in rtcd, now and again a little later.
func (p *Plugin) enforceModMute(callID, channelID, userID, sessionID, handlerID string) {
	send := func() {
		if err := p.sendSessionMute(callID, channelID, userID, sessionID, handlerID); err != nil {
			p.LogError("failed to mute session", "sessionID", sessionID, "err", err.Error())
		}
	}
	send()
	if len(modMuteResendDelays) == 0 {
		return
	}
	go func() {
		for _, d := range modMuteResendDelays {
			select {
			case <-time.After(d):
				send()
			case <-p.stopCh:
				return
			}
		}
	}()
}

// refuseUnmute keeps a moderator-muted session muted when it asks to unmute: rtcd is told it's muted and the
// client is asked to mute itself again.
func (p *Plugin) refuseUnmute(us *session, handlerID string) {
	p.LogDebug("refusing to unmute a moderator-muted session", "userID", us.userID, "channelID", us.channelID, "sessionID", us.originalConnID)
	p.enforceModMute(us.callID, us.channelID, us.userID, us.originalConnID, handlerID)
	p.publishWebSocketEvent(wsEventHostMute, map[string]interface{}{
		"channel_id": us.channelID,
		"session_id": us.originalConnID,
	}, &WebSocketBroadcast{UserID: us.userID, ReliableClusterSend: true})
}

func (p *Plugin) publishModMute(channelID, userID, by string, muted bool) {
	p.publishWebSocketEvent(wsEventModMute, map[string]interface{}{
		"channel_id": channelID,
		"user_id":    userID,
		"muted":      muted,
		"by":         by,
	}, &WebSocketBroadcast{ChannelID: channelID, ReliableClusterSend: true})
}

// onJoinModMuted keeps someone who joins a call moderator-muted if they were: their mute is bound to the call
// again, their session muted, and everyone told.
func (p *Plugin) onJoinModMuted(state *callState, channelID, userID, sessionID, handlerID string) {
	if p.isBot(userID) {
		return
	}
	m, muted, err := p.rebindModMute(channelID, userID)
	if err != nil {
		p.LogError("failed to check moderator mute", "channelID", channelID, "userID", userID, "err", err.Error())
		return
	}
	if !muted {
		return
	}
	p.enforceModMute(state.Call.ID, channelID, userID, sessionID, handlerID)
	p.publishModMute(channelID, userID, m.By, true)
}

// onLeaveModMuted starts the grace period of someone's moderator mute once they left the call.
func (p *Plugin) onLeaveModMuted(state *callState, channelID, userID string) {
	if p.isBot(userID) || state.isUserIDInCall(userID) {
		return
	}
	p.releaseModMutes(channelID, userID)
}

// setModMuted moderator-mutes someone in the call of a channel, or lifts their moderator mute.
func (p *Plugin) setModMuted(requesterID, channelID, userID string, muted bool) error {
	if !muted {
		return p.liftModMute(requesterID, channelID, userID)
	}

	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		return fmt.Errorf("failed to lock call: %w", err)
	}
	defer p.unlockCall(channelID)

	if state == nil {
		return ErrNoCallOngoing
	}
	if !state.isUserIDInCall(userID) {
		return ErrNotInCall
	}
	if err := p.checkCanModMute(requesterID, userID, channelID, state); err != nil {
		return err
	}

	if _, err := p.updateModMutes(channelID, func(mutes modMutes, now int64) bool {
		mutes[userID] = modMute{By: requesterID, At: now}
		return true
	}); err != nil {
		return err
	}

	for _, session := range state.sessions {
		if session.UserID != userID {
			continue
		}
		if session.Unmuted {
			session.Unmuted = false
			if err := p.store.UpdateCallSession(session); err != nil {
				p.LogError("failed to update call session", "err", err.Error())
			}
			bc, _ := p.sessionStateBroadcast(channelID, sessionsGetter(state.sessions))
			bc.ReliableClusterSend = true
			p.publishWebSocketEvent(wsEventUserMuted, map[string]interface{}{
				"userID":     userID,
				"session_id": session.ID,
			}, bc)
		}
		p.enforceModMute(state.Call.ID, channelID, userID, session.ID, state.Call.Props.NodeID)
		p.publishWebSocketEvent(wsEventHostMute, map[string]interface{}{
			"channel_id": channelID,
			"session_id": session.ID,
		}, &WebSocketBroadcast{UserID: userID, ReliableClusterSend: true})
	}

	p.publishModMute(channelID, userID, requesterID, true)
	return nil
}

// liftModMute lifts someone's moderator mute, in the call or within its grace period: by those who may moderate
// them, and who rank at least as high as whoever muted them.
func (p *Plugin) liftModMute(requesterID, channelID, userID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return fmt.Errorf("failed to get call state: %w", err)
	}

	mutes, _, err := p.readModMutes(channelID)
	if err != nil {
		return err
	}
	mutes.prune(time.Now().UnixMilli())
	m, ok := mutes[userID]
	if !ok {
		return nil
	}

	if err := p.checkCanModMute(requesterID, userID, channelID, state); err != nil {
		return err
	}
	if m.By != requesterID {
		channel, appErr := p.API.GetChannel(channelID)
		if appErr != nil {
			return fmt.Errorf("failed to get channel: %w", appErr)
		}
		if p.requesterRank(requesterID, channel, state) < p.moderationRank(m.By, channel) {
			return ErrOutranked
		}
	}

	if _, err := p.updateModMutes(channelID, func(mutes modMutes, _ int64) bool {
		if _, ok := mutes[userID]; !ok {
			return false
		}
		delete(mutes, userID)
		return true
	}); err != nil {
		return err
	}

	p.publishModMute(channelID, userID, requesterID, false)
	return nil
}

// askToUnmute asks someone muted in a call to unmute, at most once every askUnmuteInterval.
func (p *Plugin) askToUnmute(requesterID, channelID, sessionID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return fmt.Errorf("failed to get call state: %w", err)
	}
	if state == nil {
		return ErrNoCallOngoing
	}
	if !p.canModerateCall(requesterID, channelID, state) {
		return ErrNoPermissions
	}

	ust, ok := state.sessions[sessionID]
	if !ok {
		return ErrNotInCall
	}
	if ust.UserID == requesterID {
		return fmt.Errorf("%w: can't ask yourself", ErrNotAllowed)
	}
	if ust.Unmuted {
		return fmt.Errorf("%w: already unmuted", ErrNotAllowed)
	}
	if p.isModMuted(channelID, ust.UserID) {
		return fmt.Errorf("%w: muted by a moderator", ErrNotAllowed)
	}

	ok, appErr := p.API.KVSetWithOptions(askUnmutePrefix+channelID+"_"+ust.UserID, []byte{1}, model.PluginKVSetOptions{
		Atomic:          true,
		OldValue:        nil,
		ExpireInSeconds: int64(askUnmuteInterval.Seconds()),
	})
	if appErr != nil {
		return fmt.Errorf("failed to save the request: %w", appErr)
	}
	if !ok {
		return ErrRateLimited
	}

	p.publishWebSocketEvent(wsEventAskUnmute, map[string]interface{}{
		"channel_id": channelID,
		"session_id": sessionID,
		"by":         requesterID,
	}, &WebSocketBroadcast{UserID: ust.UserID, ReliableClusterSend: true})

	return nil
}

// clientModMuted gives the moderator mutes of a channel as clients see them: who, by whom.
func (p *Plugin) clientModMuted(channelID string) map[string]string {
	mutes, err := p.getModMutes(channelID)
	if err != nil {
		p.LogError("failed to get moderator mutes", "channelID", channelID, "err", err.Error())
		return nil
	}
	if len(mutes) == 0 {
		return nil
	}
	res := make(map[string]string, len(mutes))
	for userID, m := range mutes {
		res[userID] = m.By
	}
	return res
}

// getClientStateFor is a call's state for a client, with its moderator mutes.
func (p *Plugin) getClientStateFor(cs *callState, userID string) *CallStateClient {
	st := cs.getClientState(p.getBotID(), userID)
	st.ModMuted = p.clientModMuted(cs.Call.ChannelID)
	return st
}

// modMuteEnforcer keeps the moderator-muted sessions whose media this node handles muted in rtcd, in case a
// client added a voice track without asking to unmute.
func (p *Plugin) modMuteEnforcer() {
	ticker := time.NewTicker(modMuteEnforceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.enforceModMutes()
		case <-p.stopCh:
			return
		}
	}
}

func (p *Plugin) enforceModMutes() {
	p.mut.RLock()
	sessions := make([]*session, 0, len(p.sessions))
	for _, us := range p.sessions {
		if us.rtc || p.rtcdManager != nil {
			sessions = append(sessions, us)
		}
	}
	p.mut.RUnlock()

	seen := make(map[string]bool, len(sessions))
	for _, us := range sessions {
		if seen[us.originalConnID] {
			continue
		}
		seen[us.originalConnID] = true
		if !p.isModMuted(us.channelID, us.userID) {
			continue
		}
		if err := p.sendRTCMessage(rtc.Message{SessionID: us.originalConnID, Type: rtc.MuteMessage}, us.callID); err != nil {
			p.LogError("failed to mute session", "sessionID", us.originalConnID, "err", err.Error())
		}
	}
}
