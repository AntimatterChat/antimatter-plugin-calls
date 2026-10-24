// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"fmt"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/db"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

var (
	ErrNoCallOngoing = errors.New("no call ongoing")
	ErrNoPermissions = errors.New("no permissions")
	ErrNotInCall     = errors.New("requested session or user is not in the call")
	ErrNotAllowed    = errors.New("not allowed")
)

func (p *Plugin) changeHost(requesterID, channelID, newHostID string) error {
	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		return fmt.Errorf("failed to lock call: %w", err)
	}
	defer p.unlockCall(channelID)

	if state == nil {
		return ErrNoCallOngoing
	}

	if requesterID != state.Call.GetHostID() {
		if isAdmin := p.API.HasPermissionTo(requesterID, model.PermissionManageSystem); !isAdmin {
			return ErrNoPermissions
		}
	}

	if newHostID == p.getBotID() {
		return errors.Wrap(ErrNotAllowed, "cannot assign the bot to be host")
	}

	if state.Call.GetHostID() == newHostID {
		// Host is same, but do we need to host lock?
		if state.Call.Props.HostLockedUserID == "" {
			state.Call.Props.HostLockedUserID = newHostID
			if err := p.store.UpdateCall(&state.Call); err != nil {
				return fmt.Errorf("failed to update call: %w", err)
			}
		}
		return nil
	}

	if !state.isUserIDInCall(newHostID) {
		return ErrNotInCall
	}

	state.Call.Props.Hosts = []string{newHostID}
	state.Call.Props.HostLockedUserID = newHostID

	if err := p.store.UpdateCall(&state.Call); err != nil {
		return fmt.Errorf("failed to update call: %w", err)
	}

	p.publishWebSocketEvent(wsEventCallHostChanged, map[string]interface{}{
		"hostID":  newHostID,
		"call_id": state.Call.ID,
	}, &WebSocketBroadcast{
		ChannelID:           channelID,
		ReliableClusterSend: true,
		UserIDs:             getUserIDsFromSessions(state.sessions),
	})

	return nil
}

func (p *Plugin) muteSession(requesterID, channelID, sessionID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return err
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

	// Nobody mutes someone at or above their rank, as for the moderator mute; themselves they may.
	if ust.UserID != requesterID {
		channel, appErr := p.API.GetChannel(channelID)
		if appErr != nil {
			return fmt.Errorf("failed to get channel: %w", appErr)
		}
		if err := p.checkOutranks(requesterID, ust.UserID, channel, state); err != nil {
			return err
		}
	}

	if !ust.Unmuted {
		return nil
	}

	p.publishWebSocketEvent(wsEventHostMute, map[string]interface{}{
		"channel_id": channelID,
		"session_id": sessionID,
	}, &WebSocketBroadcast{UserID: ust.UserID, ReliableClusterSend: true})

	return nil
}

func (p *Plugin) muteOthers(requesterID, channelID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return err
	}

	if state == nil {
		return ErrNoCallOngoing
	}

	if !p.canModerateCall(requesterID, channelID, state) {
		return ErrNoPermissions
	}

	// Unmute anyone muted (who is not the host/requester).
	// If there are no unmuted sessions, return without doing anything.
	for id, s := range state.sessions {
		if s.Unmuted && s.UserID != requesterID {
			p.publishWebSocketEvent(wsEventHostMute, map[string]interface{}{
				"channel_id": channelID,
				"session_id": id,
			}, &WebSocketBroadcast{UserID: s.UserID, ReliableClusterSend: true})
		}
	}

	return nil
}

func (p *Plugin) screenOff(requesterID, channelID, sessionID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return err
	}

	if state == nil {
		return ErrNoCallOngoing
	}

	if !p.canModerateCall(requesterID, channelID, state) {
		return ErrNoPermissions
	}

	if state.Props.ScreenSharingSessionID != sessionID {
		return nil
	}

	ust, ok := state.sessions[sessionID]
	if !ok {
		return ErrNotInCall
	}

	p.publishWebSocketEvent(wsEventHostScreenOff, map[string]interface{}{
		"channel_id": channelID,
		"session_id": sessionID,
	}, &WebSocketBroadcast{UserID: ust.UserID, ReliableClusterSend: true})

	return nil
}

func (p *Plugin) lowerHand(requesterID, channelID, sessionID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return err
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

	if ust.RaisedHand == 0 {
		return nil
	}

	p.publishWebSocketEvent(wsEventHostLowerHand, map[string]interface{}{
		"call_id":    state.Call.ID,
		"channel_id": channelID,
		"session_id": sessionID,
		"host_id":    requesterID,
	}, &WebSocketBroadcast{UserID: ust.UserID, ReliableClusterSend: true})

	return nil
}

func (p *Plugin) hostRemoveSession(requesterID, channelID, sessionID string) error {
	state, err := p.getCallState(channelID, false)
	if err != nil {
		return err
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

	// Nobody disconnects someone at or above their rank, as for the moderator mute; their own sessions they may.
	if ust.UserID != requesterID {
		channel, appErr := p.API.GetChannel(channelID)
		if appErr != nil {
			return fmt.Errorf("failed to get channel: %w", appErr)
		}
		if err := p.checkOutranks(requesterID, ust.UserID, channel, state); err != nil {
			return err
		}
	}

	// Here we purposely broadcast to all the connected participants in order
	// to show the "User was removed from the call" notice.
	p.publishWebSocketEvent(wsEventHostRemoved, map[string]interface{}{
		"call_id":    state.Call.ID,
		"channel_id": channelID,
		"session_id": sessionID,
		"user_id":    ust.UserID,
	}, &WebSocketBroadcast{
		ChannelID:           channelID,
		ReliableClusterSend: true,
		UserIDs:             getUserIDsFromSessions(state.sessions),
	})

	go func() {
		// Wait a few seconds for the client to end their session cleanly. If they don't (like for an
		// older mobile client) then forcibly end it.
		time.Sleep(3 * time.Second)

		state, err := p.getCallState(channelID, false)
		if err != nil {
			p.LogError("hostRemoveSession: failed to get call state", "err", err.Error())
		}

		if state == nil {
			return
		}

		ust, ok := state.sessions[sessionID]
		if !ok {
			return
		}

		if err := p.closeRTCSession(ust.UserID, sessionID, channelID, state.Call.Props.NodeID, state.Call.ID); err != nil {
			p.LogError("hostRemoveSession: failed to close RTC session", "err", err.Error())
		}
	}()

	return nil
}

func (p *Plugin) hostEnd(requesterID, channelID string) error {
	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		return fmt.Errorf("failed to lock call: %w", err)
	}
	defer p.unlockCall(channelID)

	if state == nil {
		return ErrNoCallOngoing
	}

	if !p.canEndCall(requesterID, channelID, state) {
		return ErrNoPermissions
	}

	// Ask clients to disconnect themselves. The last to disconnect will cause the call to end, as usual.
	p.publishWebSocketEvent(wsEventCallEnd, map[string]interface{}{}, &WebSocketBroadcast{ChannelID: channelID, ReliableClusterSend: true})

	callID := state.Call.ID
	nodeID := state.Call.Props.NodeID

	go func() {
		// We wait a few seconds for the call to end cleanly. If this doesn't
		// happen we force end it.
		select {
		case <-time.After(5 * time.Second):
		case <-p.stopCh:
			return
		}

		call, err := p.store.GetCall(callID, db.GetCallOpts{})
		if err != nil {
			p.LogError("failed to get call", "err", err.Error())
		}

		sessions, err := p.store.GetCallSessions(callID, db.GetCallSessionOpts{})
		if err != nil {
			p.LogError("failed to get call sessions", "err", err.Error())
		}

		for _, session := range sessions {
			if err := p.closeRTCSession(session.UserID, session.ID, channelID, nodeID, callID); err != nil {
				p.LogError(err.Error())
			}
		}

		if err := p.cleanCallState(call); err != nil {
			p.LogError(err.Error())
		}
	}()

	return nil
}

// canEndCall tells whether someone may end the call of a channel for everyone. Antimatter: its moderators (see
// canModerateCall: its host, system admins, the channel's and the community's admins, and in voice channels the
// community's moderators), and outside voice channels whoever started it, also once they left it: leaving passes
// the host on, and the call's card then offers them to end it for everyone.
func (p *Plugin) canEndCall(requesterID, channelID string, state *callState) bool {
	if requesterID == "" || state == nil {
		return false
	}
	if requesterID == state.Call.OwnerID && !p.isVoiceRoom(channelID) {
		return true
	}
	return p.canModerateCall(requesterID, channelID, state)
}
