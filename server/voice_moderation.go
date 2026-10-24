// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/rtcd/service/rtc"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"
)

// Antimatter: the voice moderation that goes with the moderator mute (see moderation.go), as Discord's server deafen
// and move.
//
// A moderator deafen keeps someone in a voice channel from hearing and from speaking until it's lifted (deafening is
// a voice channel thing: other calls have none). It's kept with the moderator mutes of the channel, and lasts as
// long: until lifted. The Calls server enforces it: rtcd forwards no audio to the person's sessions (Antimatter's
// rtcd, see third_party/rtcd/service/rtc/deafen.go), and they're muted as by a moderator mute. Lifting it gives them
// the call's audio back, and leaves them muted (and moderator-muted, if they were also muted).
//
// Moving someone from the call of a voice channel to another voice channel of the community asks their app to switch
// calls; whoever is still in the first call after moveDisconnectDelay is disconnected from it.

const (
	wsEventModDeafen = "mod_deafen"
	wsEventHostMove  = "host_move"
)

// moveDisconnectDelay is how long someone moved to another voice channel has to leave the call they were in.
var moveDisconnectDelay = 5 * time.Second

func (p *Plugin) publishModDeafen(channelID, userID, by string, deafened bool) {
	p.publishWebSocketEvent(wsEventModDeafen, map[string]interface{}{
		"channel_id": channelID,
		"user_id":    userID,
		"deafened":   deafened,
		"by":         by,
	}, &WebSocketBroadcast{ChannelID: channelID, ReliableClusterSend: true})
}

// enforceModDeafen deafens (or undeafens) a session in rtcd, through the node handling its call's media.
func (p *Plugin) enforceModDeafen(callID, channelID, userID, sessionID, handlerID string, deafened bool) {
	msgType := rtc.UndeafenMessage
	if deafened {
		msgType = rtc.DeafenMessage
	}
	if err := p.sendSessionRTC(msgType, callID, channelID, userID, sessionID, handlerID); err != nil {
		p.LogError("failed to set the deafened state of a session", "sessionID", sessionID, "deafened", deafened, "err", err.Error())
	}
}

// enforceModDeafenOf keeps a session whose media this node handles deafened in rtcd while its user is
// moderator-deafened, and undeafens it once that's lifted (should the message of the lifting have been lost).
func (p *Plugin) enforceModDeafenOf(us *session) {
	deafened := p.isModDeafened(us.channelID, us.userID)
	if !deafened && !us.rtcDeafened.Load() {
		return
	}
	msgType := rtc.UndeafenMessage
	if deafened {
		msgType = rtc.DeafenMessage
	}
	if err := p.sendRTCMessage(rtc.Message{SessionID: us.originalConnID, Type: msgType}, us.callID); err != nil {
		p.LogError("failed to set the deafened state of a session", "sessionID", us.originalConnID, "deafened", deafened, "err", err.Error())
		return
	}
	us.rtcDeafened.Store(deafened)
}

// checkCanLift tells whether a requester may lift a moderator mute or deafen that someone (by) gave: those who may
// moderate the person, and who rank at least as high as whoever gave it.
func (p *Plugin) checkCanLift(requesterID, userID, channelID, by string, state *callState) error {
	if err := p.checkCanModMute(requesterID, userID, channelID, state); err != nil {
		return err
	}
	if by == requesterID {
		return nil
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return fmt.Errorf("failed to get channel: %w", appErr)
	}
	if p.requesterRank(requesterID, channel, state) < p.moderationRank(by, channel) {
		return ErrOutranked
	}
	return nil
}

// setModDeafened moderator-deafens someone in the call of a voice channel, or lifts their moderator deafen.
func (p *Plugin) setModDeafened(requesterID, channelID, userID string, deafened bool) error {
	if !deafened {
		return p.liftModDeafen(requesterID, channelID, userID)
	}

	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		return fmt.Errorf("failed to lock call: %w", err)
	}
	defer p.unlockCall(channelID)

	if state == nil {
		return ErrNoCallOngoing
	}
	if !p.isVoiceRoom(channelID) {
		return fmt.Errorf("%w: only in voice channels", ErrNotAllowed)
	}
	if !state.isUserIDInCall(userID) {
		return ErrNotInCall
	}
	if err := p.checkCanModMute(requesterID, userID, channelID, state); err != nil {
		return err
	}

	if _, err := p.updateModMutes(channelID, func(mutes modMutes, now int64) bool {
		m := mutes[userID]
		m.DeafBy, m.DeafAt, m.Until = requesterID, now, 0
		mutes[userID] = m
		return true
	}); err != nil {
		return err
	}

	p.muteUserSessions(state, channelID, userID)
	for _, session := range state.sessions {
		if session.UserID == userID {
			p.enforceModDeafen(state.Call.ID, channelID, userID, session.ID, state.Call.Props.NodeID, true)
		}
	}

	p.publishModDeafen(channelID, userID, requesterID, true)
	return nil
}

// liftModDeafen lifts someone's moderator deafen, also while they're out of the call: by those who may moderate
// them, and who rank at least as high as whoever deafened them. They stay muted.
func (p *Plugin) liftModDeafen(requesterID, channelID, userID string) error {
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
	if !ok || !m.isDeafened() {
		return nil
	}
	if err := p.checkCanLift(requesterID, userID, channelID, m.DeafBy, state); err != nil {
		return err
	}

	if _, err := p.updateModMutes(channelID, func(mutes modMutes, _ int64) bool {
		m, ok := mutes[userID]
		if !ok || !m.isDeafened() {
			return false
		}
		m.DeafBy, m.DeafAt = "", 0
		if m.isMuted() {
			mutes[userID] = m
		} else {
			delete(mutes, userID)
		}
		return true
	}); err != nil {
		return err
	}

	if state != nil {
		for _, session := range state.sessions {
			if session.UserID == userID {
				p.enforceModDeafen(state.Call.ID, channelID, userID, session.ID, state.Call.Props.NodeID, false)
			}
		}
	}

	p.publishModDeafen(channelID, userID, requesterID, false)
	return nil
}

// isOpenVoiceRoom tells whether a channel is a voice channel with calls turned on.
func (p *Plugin) isOpenVoiceRoom(channelID string) bool {
	cc := p.getCachedCallsChannel(channelID)
	return cc != nil && cc.Enabled && cc.BoolProp(public.ChannelPropBroadcastSessionState)
}

// moveSession moves someone in the call of a voice channel to another voice channel of the same community: their
// app is asked to switch to its call, and they're disconnected from the first call if still in it after
// moveDisconnectDelay. By those who may moderate them in the first channel (as for the moderator mute) and who may
// moderate calls in the second; the person must be allowed to join the second.
func (p *Plugin) moveSession(requesterID, channelID, sessionID, toChannelID string) error {
	if toChannelID == channelID {
		return fmt.Errorf("%w: already in that channel", ErrNotAllowed)
	}

	state, err := p.getCallState(channelID, false)
	if err != nil {
		return fmt.Errorf("failed to get call state: %w", err)
	}
	if state == nil {
		return ErrNoCallOngoing
	}
	ust, ok := state.sessions[sessionID]
	if !ok {
		return ErrNotInCall
	}
	if err := p.checkCanModMute(requesterID, ust.UserID, channelID, state); err != nil {
		return err
	}

	from, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return fmt.Errorf("failed to get channel: %w", appErr)
	}
	to, appErr := p.API.GetChannel(toChannelID)
	if appErr != nil || to == nil {
		return fmt.Errorf("%w: no such channel", ErrNotAllowed)
	}
	if from.TeamId == "" || to.TeamId != from.TeamId || to.DeleteAt > 0 || !p.isVoiceRoom(channelID) || !p.isOpenVoiceRoom(toChannelID) {
		return fmt.Errorf("%w: only between the voice channels of a community", ErrNotAllowed)
	}
	if p.moderationRank(requesterID, to) == modRankNone {
		return ErrNoPermissions
	}
	if !p.API.HasPermissionToChannel(ust.UserID, toChannelID, model.PermissionCreatePost) {
		return fmt.Errorf("%w: they can't join that channel", ErrNotAllowed)
	}

	p.publishWebSocketEvent(wsEventHostMove, map[string]interface{}{
		"channel_id":    channelID,
		"session_id":    sessionID,
		"to_channel_id": toChannelID,
		"by":            requesterID,
	}, &WebSocketBroadcast{UserID: ust.UserID, ReliableClusterSend: true})

	go p.disconnectAfter(channelID, sessionID, moveDisconnectDelay)

	return nil
}

// disconnectAfter disconnects a session from the call of a channel if it's still in it after a while.
func (p *Plugin) disconnectAfter(channelID, sessionID string, d time.Duration) {
	select {
	case <-time.After(d):
	case <-p.stopCh:
		return
	}

	state, err := p.getCallState(channelID, false)
	if err != nil {
		p.LogError("failed to get call state", "channelID", channelID, "err", err.Error())
		return
	}
	if state == nil {
		return
	}
	ust, ok := state.sessions[sessionID]
	if !ok {
		return
	}
	if err := p.closeRTCSession(ust.UserID, sessionID, channelID, state.Call.Props.NodeID, state.Call.ID); err != nil {
		p.LogError("failed to close RTC session", "sessionID", sessionID, "err", err.Error())
	}
}

// modMuteInfo is a moderator mute or deafen of a channel as its moderators see it in the voice channel's settings,
// with whether the requester may lift it.
type modMuteInfo struct {
	UserID        string `json:"user_id"`
	MutedBy       string `json:"muted_by,omitempty"`
	MutedAt       int64  `json:"muted_at,omitempty"`
	DeafenedBy    string `json:"deafened_by,omitempty"`
	DeafenedAt    int64  `json:"deafened_at,omitempty"`
	CanLiftMute   bool   `json:"can_lift_mute"`
	CanLiftDeafen bool   `json:"can_lift_deafen"`
}

// listModMutes lists the moderator mutes and deafens of a channel, the latest first, also of people who aren't in
// its call (in voice channels they last until lifted): for those who may moderate its calls, not as a call's host.
func (p *Plugin) listModMutes(requesterID, channelID string) ([]modMuteInfo, error) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil || channel == nil {
		return nil, ErrNoPermissions
	}
	if p.moderationRank(requesterID, channel) == modRankNone {
		return nil, ErrNoPermissions
	}

	mutes, _, err := p.readModMutes(channelID)
	if err != nil {
		return nil, err
	}
	mutes.prune(time.Now().UnixMilli())

	list := make([]modMuteInfo, 0, len(mutes))
	for userID, m := range mutes {
		info := modMuteInfo{UserID: userID, MutedBy: m.By, MutedAt: m.At, DeafenedBy: m.DeafBy, DeafenedAt: m.DeafAt}
		if m.isMuted() {
			info.CanLiftMute = p.checkCanLift(requesterID, userID, channelID, m.By, nil) == nil
		}
		if m.isDeafened() {
			info.CanLiftDeafen = p.checkCanLift(requesterID, userID, channelID, m.DeafBy, nil) == nil
		}
		list = append(list, info)
	}
	latest := func(i modMuteInfo) int64 { return max(i.MutedAt, i.DeafenedAt) }
	sort.Slice(list, func(i, j int) bool {
		if a, b := latest(list[i]), latest(list[j]); a != b {
			return a > b
		}
		return list[i].UserID < list[j].UserID
	})
	return list, nil
}
