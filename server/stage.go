// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// Antimatter: stages (CHANNEL_TYPES_DESIGN.md §3.5). A stage is a voice channel whose am_mode is "stage". Its
// speakers are those who may moderate its calls (moderationRank above modRankNone) and the people a moderator invited
// to speak; everyone else is the audience. The audience is muted and can't unmute (as with a moderator mute: unmuting
// is refused and rtcd told the session is muted), can't share their screen or turn their camera on, and hears
// everything. Raising a hand is how they ask to speak. The host is only ever a speaker: with no speaker in the call,
// there is no host.
//
// The speakers are kept in the KV store (stage_speakers_<channelId>), the moderators among them too (marked mod) so
// that listing them and electing a host take no permission checks: they are added as they join (or when the channel
// becomes a stage). Like moderator mutes, an entry lasts while the person is in the call and for modMuteGrace after
// they left it, so that rejoining keeps them a speaker; once the call has been empty that long, the key is gone.

const (
	wsEventStageSpeakersChanged = "stage_speakers_changed"
	wsEventStageInvited         = "stage_invited"
	stageSpeakersKeyPrefix      = "stage_speakers_"

	// The channel prop, filled in by the server from the channel's type, and its value for stages.
	channelPropMode  = "am_mode"
	channelModeStage = "stage"

	// How long whether a channel is a stage, and its speakers, are cached: a mode change applies within this long.
	stageCacheTTL = 5 * time.Second

	// How long the speakers are kept while someone is still bound to the call (a node lost mid-call can't release
	// them).
	stageSpeakersKeyTTL = 24 * time.Hour

	stageUpdateAttempts = 5
)

// stageSpeaker is a speaker of a stage.
type stageSpeaker struct {
	// When they became one.
	Since int64 `json:"since"`

	// When it ends: 0 while they're in the call, then modMuteGrace after they left it.
	Until int64 `json:"until,omitempty"`

	// They speak by their role (they may moderate the stage's calls), not by an invitation.
	Mod bool `json:"mod,omitempty"`
}

func (s stageSpeaker) isActive(now int64) bool {
	return s.Until == 0 || now < s.Until
}

// stageSpeakers are the speakers of a stage, by user.
type stageSpeakers map[string]stageSpeaker

func (ss stageSpeakers) prune(now int64) bool {
	pruned := false
	for userID, s := range ss {
		if !s.isActive(now) {
			delete(ss, userID)
			pruned = true
		}
	}
	return pruned
}

// ttl is how long the key of the speakers is kept: until the last one's grace period ends, or a day while someone
// is in the call.
func (ss stageSpeakers) ttl(now int64) int64 {
	var last int64
	for _, s := range ss {
		if s.Until == 0 {
			return int64(stageSpeakersKeyTTL.Seconds())
		}
		last = max(last, s.Until)
	}
	return max(1, (last-now+999)/1000)
}

// ids lists the speakers, sorted.
func (ss stageSpeakers) ids() []string {
	ids := make([]string, 0, len(ss))
	for userID := range ss {
		ids = append(ids, userID)
	}
	slices.Sort(ids)
	return ids
}

func stageSpeakersKey(channelID string) string {
	return stageSpeakersKeyPrefix + channelID
}

// channelIsStage tells whether a channel's mode is stage (only voice channels have one).
func channelIsStage(channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	mode, _ := channel.Props[channelPropMode].(string)
	return mode == channelModeStage
}

// stageCache keeps, for a few seconds, whether channels are stages and their speakers; and, for the enforcer, which
// channels this node last saw as stages. Its zero value is ready to use.
type stageCache struct {
	mut      sync.Mutex
	modes    map[string]stageModeEntry
	speakers map[string]stageSpeakersEntry
	seen     map[string]bool
}

type stageModeEntry struct {
	stage     bool
	expiresAt time.Time
}

type stageSpeakersEntry struct {
	speakers  stageSpeakers
	expiresAt time.Time
}

func (c *stageCache) mode(channelID string, now time.Time) (bool, bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	entry, ok := c.modes[channelID]
	if !ok || now.After(entry.expiresAt) {
		return false, false
	}
	return entry.stage, true
}

func (c *stageCache) setMode(channelID string, stage bool, now time.Time) {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.modes == nil {
		c.modes = map[string]stageModeEntry{}
	}
	for id, entry := range c.modes {
		if now.After(entry.expiresAt) {
			delete(c.modes, id)
		}
	}
	c.modes[channelID] = stageModeEntry{stage: stage, expiresAt: now.Add(stageCacheTTL)}
}

func (c *stageCache) getSpeakers(channelID string, now time.Time) (stageSpeakers, bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	entry, ok := c.speakers[channelID]
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return entry.speakers, true
}

func (c *stageCache) setSpeakers(channelID string, speakers stageSpeakers, now time.Time) {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.speakers == nil {
		c.speakers = map[string]stageSpeakersEntry{}
	}
	for id, entry := range c.speakers {
		if now.After(entry.expiresAt) {
			delete(c.speakers, id)
		}
	}
	c.speakers[channelID] = stageSpeakersEntry{speakers: speakers, expiresAt: now.Add(stageCacheTTL)}
}

// sawStage records whether this node saw a channel as a stage, telling what it saw before.
func (c *stageCache) sawStage(channelID string, stage bool) (before, known bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	before, known = c.seen[channelID]
	c.seen[channelID] = stage
	return before, known
}

// forgetSeen drops the channels that this node no longer has sessions in.
func (c *stageCache) forgetSeen(keep map[string]bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	for id := range c.seen {
		if !keep[id] {
			delete(c.seen, id)
		}
	}
}

// isStage tells whether a channel is a stage: a voice channel with the stage mode (cached for stageCacheTTL).
func (p *Plugin) isStage(channelID string) bool {
	return p.isStageAt(channelID, time.Now())
}

func (p *Plugin) isStageAt(channelID string, now time.Time) bool {
	if channelID == "" || !p.isVoiceRoom(channelID) {
		return false
	}
	if stage, ok := p.stageCache.mode(channelID, now); ok {
		return stage
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.LogError("failed to get channel", "channelID", channelID, "err", appErr.Error())
		return false
	}
	stage := channelIsStage(channel)
	p.stageCache.setMode(channelID, stage, now)
	return stage
}

// readStageSpeakers reads the speakers of a stage as stored, with the stored value for compare-and-set.
func (p *Plugin) readStageSpeakers(channelID string) (stageSpeakers, []byte, error) {
	var data []byte
	if p.store != nil {
		var err error
		if data, err = p.KVGet(stageSpeakersKey(channelID), true); err != nil {
			return nil, nil, fmt.Errorf("failed to get stage speakers: %w", err)
		}
	} else {
		var appErr *model.AppError
		if data, appErr = p.API.KVGet(stageSpeakersKey(channelID)); appErr != nil {
			return nil, nil, fmt.Errorf("failed to get stage speakers: %w", appErr)
		}
	}

	speakers := stageSpeakers{}
	if len(data) == 0 {
		return speakers, nil, nil
	}
	if err := json.Unmarshal(data, &speakers); err != nil {
		p.LogWarn("dropping unreadable stage speakers", "channelID", channelID, "err", err.Error())
		return stageSpeakers{}, data, nil
	}
	return speakers, data, nil
}

// getStageSpeakers gives the speakers of a stage: as stored when fresh, else possibly cached for a few seconds. Don't
// change it.
func (p *Plugin) getStageSpeakers(channelID string, fresh bool) (stageSpeakers, error) {
	now := time.Now()
	if !fresh {
		if speakers, ok := p.stageCache.getSpeakers(channelID, now); ok {
			return speakers, nil
		}
	}
	speakers, _, err := p.readStageSpeakers(channelID)
	if err != nil {
		return nil, err
	}
	speakers.prune(now.UnixMilli())
	p.stageCache.setSpeakers(channelID, speakers, now)
	return speakers, nil
}

// updateStageSpeakers changes the speakers of a stage atomically; change tells whether it changed anything. It
// returns the speakers afterwards, and whether they changed.
func (p *Plugin) updateStageSpeakers(channelID string, change func(speakers stageSpeakers, now int64) bool) (stageSpeakers, bool, error) {
	key := stageSpeakersKey(channelID)
	for i := 0; i < stageUpdateAttempts; i++ {
		speakers, old, err := p.readStageSpeakers(channelID)
		if err != nil {
			return nil, false, err
		}
		now := time.Now()
		pruned := speakers.prune(now.UnixMilli())
		changed := change(speakers, now.UnixMilli())
		if !changed && !pruned {
			p.stageCache.setSpeakers(channelID, speakers, now)
			return speakers, false, nil
		}

		var ok bool
		var appErr *model.AppError
		if len(speakers) == 0 && old == nil {
			return speakers, changed, nil
		} else if len(speakers) == 0 {
			ok, appErr = p.API.KVCompareAndDelete(key, old)
		} else {
			data, err := json.Marshal(speakers)
			if err != nil {
				return nil, false, fmt.Errorf("failed to marshal stage speakers: %w", err)
			}
			ok, appErr = p.API.KVSetWithOptions(key, data, model.PluginKVSetOptions{
				Atomic:          true,
				OldValue:        old,
				ExpireInSeconds: speakers.ttl(now.UnixMilli()),
			})
		}
		if appErr != nil {
			return nil, false, fmt.Errorf("failed to save stage speakers: %w", appErr)
		}
		if ok {
			p.stageCache.setSpeakers(channelID, speakers, now)
			return speakers, changed, nil
		}
		time.Sleep(time.Duration(10*(i+1)) * time.Millisecond)
	}
	return nil, false, fmt.Errorf("failed to save stage speakers: too many concurrent changes")
}

// isStageSpeaker tells whether someone may speak on a stage: invited to, or by their role. Live, it checks their
// role when they aren't listed (and lists them if they may moderate the stage's calls, or unlists them if they no
// longer may); otherwise it only reads the list, possibly cached.
func (p *Plugin) isStageSpeaker(channel *model.Channel, userID string, live bool) bool {
	speakers, err := p.getStageSpeakers(channel.Id, false)
	if err != nil {
		p.LogError("failed to get stage speakers", "channelID", channel.Id, "err", err.Error())
		speakers = nil
	}
	s, listed := speakers[userID]
	if !live {
		return listed
	}
	if listed && !s.Mod {
		return true
	}
	if !listed {
		// Another node may have just invited them.
		if speakers, err = p.getStageSpeakers(channel.Id, true); err == nil {
			if s, listed = speakers[userID]; listed && !s.Mod {
				return true
			}
		}
	}

	mod := p.moderationRank(userID, channel) > modRankNone
	if mod != listed {
		if _, changed, err := p.updateStageSpeakers(channel.Id, func(speakers stageSpeakers, now int64) bool {
			s, ok := speakers[userID]
			switch {
			case mod && !ok:
				speakers[userID] = stageSpeaker{Since: now, Mod: true}
			case !mod && ok && s.Mod:
				delete(speakers, userID)
			default:
				return false
			}
			return true
		}); err != nil {
			p.LogError("failed to update stage speakers", "channelID", channel.Id, "err", err.Error())
		} else if changed {
			p.publishStageSpeakers(channel.Id)
		}
	}
	return mod
}

// isStageAudience tells whether someone in the call of a channel is in a stage's audience, so may not speak, share
// their screen or turn their camera on (checking their role when they aren't listed as a speaker).
func (p *Plugin) isStageAudience(channelID, userID string) bool {
	if p.isBot(userID) || !p.isStage(channelID) {
		return false
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.LogError("failed to get channel", "channelID", channelID, "err", appErr.Error())
		return true
	}
	return !p.isStageSpeaker(channel, userID, true)
}

// isInvitedSpeaker tells whether someone was invited to speak on a stage (rather than speaking by their role).
func (p *Plugin) isInvitedSpeaker(channelID, userID string) bool {
	speakers, err := p.getStageSpeakers(channelID, true)
	if err != nil {
		p.LogError("failed to get stage speakers", "channelID", channelID, "err", err.Error())
		return false
	}
	s, ok := speakers[userID]
	return ok && !s.Mod
}

// publishStageSpeakers tells the channel whether its call is a stage, and who its speakers are.
func (p *Plugin) publishStageSpeakers(channelID string) {
	stage := p.isStage(channelID)
	speakers := []string{}
	if stage {
		if ss, err := p.getStageSpeakers(channelID, false); err != nil {
			p.LogError("failed to get stage speakers", "channelID", channelID, "err", err.Error())
		} else {
			speakers = ss.ids()
		}
	}
	p.publishWebSocketEvent(wsEventStageSpeakersChanged, map[string]interface{}{
		"channel_id": channelID,
		"stage":      stage,
		"speakers":   speakers,
	}, &WebSocketBroadcast{ChannelID: channelID, ReliableClusterSend: true})
}

// clientStage fills in the stage of a call's state for a client.
func (p *Plugin) clientStage(st *CallStateClient, channelID string) {
	if !p.isStage(channelID) {
		return
	}
	st.Stage = true
	speakers, err := p.getStageSpeakers(channelID, false)
	if err != nil {
		p.LogError("failed to get stage speakers", "channelID", channelID, "err", err.Error())
		return
	}
	st.Speakers = speakers.ids()
}

// onJoinStage binds a speaker of a stage to its call again as they join it, or lists them as a speaker if they may
// moderate its calls. Called before the host is elected, so that a speaker joining can become it.
func (p *Plugin) onJoinStage(channelID, userID string) {
	if p.isBot(userID) || !p.isStage(channelID) {
		return
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.LogError("failed to get channel", "channelID", channelID, "err", appErr.Error())
		return
	}
	mod := p.moderationRank(userID, channel) > modRankNone
	_, changed, err := p.updateStageSpeakers(channelID, func(speakers stageSpeakers, now int64) bool {
		s, ok := speakers[userID]
		switch {
		case ok && s.Mod && !mod:
			// They no longer may moderate its calls.
			delete(speakers, userID)
		case ok && s.Until != 0:
			s.Until = 0
			speakers[userID] = s
		case !ok && mod:
			speakers[userID] = stageSpeaker{Since: now, Mod: true}
		default:
			return false
		}
		return true
	})
	if err != nil {
		p.LogError("failed to update stage speakers", "channelID", channelID, "userID", userID, "err", err.Error())
		return
	}
	if changed {
		p.publishStageSpeakers(channelID)
	}
}

// onJoinStageAudience mutes, in rtcd, someone joining a stage's call in its audience.
func (p *Plugin) onJoinStageAudience(state *callState, channelID, userID, sessionID, handlerID string) {
	if p.isBot(userID) || !p.isStage(channelID) {
		return
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.LogError("failed to get channel", "channelID", channelID, "err", appErr.Error())
		return
	}
	if !p.isStageSpeaker(channel, userID, false) {
		p.enforceModMute(state.Call.ID, channelID, userID, sessionID, handlerID)
	}
}

// releaseStageSpeakers starts the grace period of the speakers who left a channel's call: all of them when userID
// is empty (the call ended).
func (p *Plugin) releaseStageSpeakers(channelID, userID string) {
	if !p.isVoiceRoom(channelID) {
		return
	}
	if _, _, err := p.updateStageSpeakers(channelID, func(speakers stageSpeakers, now int64) bool {
		changed := false
		for id, s := range speakers {
			if (userID == "" || id == userID) && s.Until == 0 {
				s.Until = now + modMuteGrace.Milliseconds()
				speakers[id] = s
				changed = true
			}
		}
		return changed
	}); err != nil {
		p.LogError("failed to release stage speakers", "channelID", channelID, "userID", userID, "err", err.Error())
	}
}

// onLeaveStage starts the grace period of a speaker once they left the call.
func (p *Plugin) onLeaveStage(state *callState, channelID, userID string) {
	if p.isBot(userID) || state.isUserIDInCall(userID) {
		return
	}
	p.releaseStageSpeakers(channelID, userID)
}

// electHost chooses the host of a channel's call: in a stage, only among its speakers.
func (p *Plugin) electHost(state *callState, channelID string) string {
	botID := p.getBotID()
	if !p.isStage(channelID) {
		return state.getHostID(botID)
	}
	speakers, err := p.getStageSpeakers(channelID, false)
	if err != nil {
		p.LogError("failed to get stage speakers", "channelID", channelID, "err", err.Error())
		return ""
	}
	return state.getHostIDAmong(botID, func(userID string) bool {
		_, ok := speakers[userID]
		return ok
	})
}

// fixStageHost makes sure the host of a stage's call is a speaker, choosing another (or none) otherwise. Call it
// under the call's lock.
func (p *Plugin) fixStageHost(state *callState, channelID string) {
	hostID := p.electHost(state, channelID)
	if hostID == state.Call.GetHostID() {
		return
	}
	if hostID == "" {
		state.Call.Props.Hosts = nil
	} else {
		state.Call.Props.Hosts = []string{hostID}
	}
	if locked := state.Call.Props.HostLockedUserID; locked != "" && locked != hostID {
		state.Call.Props.HostLockedUserID = ""
	}
	if err := p.store.UpdateCall(&state.Call); err != nil {
		p.LogError("failed to update call", "err", err.Error())
	}
	p.publishWebSocketEvent(wsEventCallHostChanged, map[string]interface{}{
		"hostID":  hostID,
		"call_id": state.Call.ID,
	}, &WebSocketBroadcast{
		ChannelID:           channelID,
		ReliableClusterSend: true,
		UserIDs:             getUserIDsFromSessions(state.sessions),
	})
}

// silenceAudience mutes someone of a stage's audience in its call, and stops their screen share: in rtcd, and their
// app is told (it also turns their camera off, as the speakers change).
func (p *Plugin) silenceAudience(state *callState, channelID, userID string) {
	p.muteUserSessions(state, channelID, userID)
	for _, session := range state.sessions {
		if session.UserID == userID && state.Call.Props.ScreenSharingSessionID == session.ID {
			p.publishWebSocketEvent(wsEventHostScreenOff, map[string]interface{}{
				"channel_id": channelID,
				"session_id": session.ID,
			}, &WebSocketBroadcast{UserID: userID, ReliableClusterSend: true})
		}
	}
}

// startStage applies the stage to a call already going on as its channel becomes one: those who may moderate it
// become speakers, the others are muted, and the host must be a speaker.
func (p *Plugin) startStage(channelID string) {
	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		p.LogError("failed to lock call", "channelID", channelID, "err", err.Error())
		return
	}
	defer p.unlockCall(channelID)
	if state == nil {
		return
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.LogError("failed to get channel", "channelID", channelID, "err", appErr.Error())
		return
	}

	speakers, err := p.getStageSpeakers(channelID, true)
	if err != nil {
		p.LogError("failed to get stage speakers", "channelID", channelID, "err", err.Error())
		return
	}
	mods := map[string]bool{}
	for _, session := range state.sessions {
		if _, ok := speakers[session.UserID]; ok || mods[session.UserID] || p.isBot(session.UserID) {
			continue
		}
		if p.moderationRank(session.UserID, channel) > modRankNone {
			mods[session.UserID] = true
		}
	}
	speakers, _, err = p.updateStageSpeakers(channelID, func(speakers stageSpeakers, now int64) bool {
		changed := false
		for _, session := range state.sessions {
			s, ok := speakers[session.UserID]
			if ok && s.Until != 0 {
				s.Until = 0
				speakers[session.UserID] = s
				changed = true
			} else if !ok && mods[session.UserID] {
				speakers[session.UserID] = stageSpeaker{Since: now, Mod: true}
				changed = true
			}
		}
		return changed
	})
	if err != nil {
		p.LogError("failed to update stage speakers", "channelID", channelID, "err", err.Error())
		return
	}

	silenced := map[string]bool{}
	for _, session := range state.sessions {
		if _, ok := speakers[session.UserID]; ok || silenced[session.UserID] || p.isBot(session.UserID) {
			continue
		}
		if session.Unmuted || state.Call.Props.ScreenSharingSessionID == session.ID {
			silenced[session.UserID] = true
			p.silenceAudience(state, channelID, session.UserID)
		}
	}

	p.fixStageHost(state, channelID)
	p.publishStageSpeakers(channelID)
}

// enforceStages applies a channel's change of mode to its call (seen from the sessions this node has), and keeps the
// audience muted in rtcd in case an app added a voice track without asking to unmute (for the sessions whose media
// this node handles).
func (p *Plugin) enforceStages(all, media []*session) {
	channels := map[string]bool{}
	for _, us := range all {
		channels[us.channelID] = true
	}
	p.stageCache.forgetSeen(channels)

	stages := map[string]stageSpeakers{}
	for channelID := range channels {
		stage := p.isStage(channelID)
		before, known := p.stageCache.sawStage(channelID, stage)
		switch {
		case stage && (!known || !before):
			p.startStage(channelID)
		case !stage && known && before:
			p.publishStageSpeakers(channelID)
		}
		if !stage {
			continue
		}

		// Read once per channel: who was just invited must not be muted again by a stale list.
		speakers, err := p.getStageSpeakers(channelID, true)
		if err != nil {
			p.LogError("failed to get stage speakers", "channelID", channelID, "err", err.Error())
			continue
		}
		stages[channelID] = speakers
	}

	for _, us := range media {
		speakers, ok := stages[us.channelID]
		if !ok || p.isBot(us.userID) {
			continue
		}
		if _, ok := speakers[us.userID]; ok {
			continue
		}
		if err := p.sendSessionMute(us.callID, us.channelID, us.userID, us.originalConnID, p.nodeID); err != nil {
			p.LogError("failed to mute session", "sessionID", us.originalConnID, "err", err.Error())
		}
	}
}

// inviteToSpeak makes someone of a stage's audience a speaker, by those who may moderate them: their hand is lowered
// and they're told, to unmute themselves (nobody is unmuted by someone else).
func (p *Plugin) inviteToSpeak(requesterID, channelID, userID string) error {
	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		return fmt.Errorf("failed to lock call: %w", err)
	}
	defer p.unlockCall(channelID)

	if state == nil {
		return ErrNoCallOngoing
	}
	if !p.isStage(channelID) {
		return fmt.Errorf("%w: not a stage", ErrNotAllowed)
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return fmt.Errorf("failed to get channel: %w", appErr)
	}
	if err := p.checkOutranks(requesterID, userID, channel, state); err != nil {
		return err
	}
	if p.isBot(userID) {
		return ErrNotAllowed
	}
	if !state.isUserIDInCall(userID) {
		return ErrNotInCall
	}

	_, changed, err := p.updateStageSpeakers(channelID, func(speakers stageSpeakers, now int64) bool {
		if s, ok := speakers[userID]; ok && s.Until == 0 {
			return false
		}
		speakers[userID] = stageSpeaker{Since: now}
		return true
	})
	if err != nil {
		return err
	}
	if !changed {
		// Already a speaker.
		return nil
	}

	for _, session := range state.sessions {
		if session.UserID != userID || session.RaisedHand == 0 {
			continue
		}
		session.RaisedHand = 0
		if err := p.store.UpdateCallSession(session); err != nil {
			p.LogError("failed to update call session", "err", err.Error())
		}
		bc, _ := p.sessionStateBroadcast(channelID, sessionsGetter(state.sessions))
		bc.ReliableClusterSend = true
		p.publishWebSocketEvent(wsEventUserUnraiseHand, map[string]interface{}{
			"userID":      userID,
			"session_id":  session.ID,
			"raised_hand": 0,
		}, bc)
	}

	p.publishWebSocketEvent(wsEventStageInvited, map[string]interface{}{
		"channel_id": channelID,
		"by":         requesterID,
	}, &WebSocketBroadcast{UserID: userID, ReliableClusterSend: true})
	p.fixStageHost(state, channelID)
	p.publishStageSpeakers(channelID)
	return nil
}

// moveToAudience makes an invited speaker of a stage part of its audience again, muting them: by those who may
// moderate them, or by the speaker themselves. Those who speak by their role can't be moved.
func (p *Plugin) moveToAudience(requesterID, channelID, userID string) error {
	state, err := p.lockCallReturnState(channelID)
	if err != nil {
		return fmt.Errorf("failed to lock call: %w", err)
	}
	defer p.unlockCall(channelID)

	if state == nil {
		return ErrNoCallOngoing
	}
	if !p.isStage(channelID) {
		return fmt.Errorf("%w: not a stage", ErrNotAllowed)
	}
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return fmt.Errorf("failed to get channel: %w", appErr)
	}
	if requesterID != userID {
		if err := p.checkOutranks(requesterID, userID, channel, state); err != nil {
			return err
		}
	}
	if p.moderationRank(userID, channel) > modRankNone {
		return fmt.Errorf("%w: they speak by their role", ErrNotAllowed)
	}

	_, changed, err := p.updateStageSpeakers(channelID, func(speakers stageSpeakers, _ int64) bool {
		if _, ok := speakers[userID]; !ok {
			return false
		}
		delete(speakers, userID)
		return true
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if state.isUserIDInCall(userID) {
		p.silenceAudience(state, channelID, userID)
	}
	p.fixStageHost(state, channelID)
	p.publishStageSpeakers(channelID)
	return nil
}
