// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"
)

// Antimatter: calls are on by default in corporate communities (a company's) and off in communities (public or
// hobby ones), which have voice channels for talking instead. That's the community's kind (the server's Team.Kind):
// community unless set to corporate. A channel's own Calls setting (enabled or disabled by its admins, or a voice
// channel) always wins, and direct and group messages follow the server's default (test mode).

// teamKindCacheTTL is how long the kind of a community is cached: a change of kind applies after at most this long.
const teamKindCacheTTL = 30 * time.Second

type teamKindCacheEntry struct {
	community bool
	expiresAt time.Time
}

// teamKindCache caches whether teams are communities (rather than corporate). Its zero value is ready to use.
type teamKindCache struct {
	mut     sync.Mutex
	entries map[string]teamKindCacheEntry
}

func (c *teamKindCache) get(teamID string, now time.Time) (community, ok bool) {
	c.mut.Lock()
	defer c.mut.Unlock()
	entry, found := c.entries[teamID]
	if !found || !now.Before(entry.expiresAt) {
		return false, false
	}
	return entry.community, true
}

func (c *teamKindCache) set(teamID string, community bool, now time.Time) {
	c.mut.Lock()
	defer c.mut.Unlock()
	if c.entries == nil {
		c.entries = map[string]teamKindCacheEntry{}
	}
	for id, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, id)
		}
	}
	c.entries[teamID] = teamKindCacheEntry{community: community, expiresAt: now.Add(teamKindCacheTTL)}
}

// communityTeams tells which of the given teams are communities (not corporate), possibly cached. A team whose kind
// can't be read counts as neither: calls there follow the server's default.
func (p *Plugin) communityTeams(teamIDs []string) map[string]bool {
	now := time.Now()
	result := make(map[string]bool, len(teamIDs))
	var missing []string
	for _, id := range teamIDs {
		if id == "" {
			continue
		}
		if community, ok := p.teamKinds.get(id, now); ok {
			result[id] = community
			continue
		}
		result[id] = false
		missing = append(missing, id)
	}
	if len(missing) == 0 || p.store == nil {
		return result
	}

	corporate, err := p.store.GetCorporateTeams(missing)
	if err != nil {
		p.LogError("failed to get the kind of communities", "err", err.Error())
		return result
	}
	for _, id := range missing {
		result[id] = !corporate[id]
		p.teamKinds.set(id, result[id], now)
	}
	return result
}

// isCommunityTeam tells whether a team is a community (not corporate), possibly cached.
func (p *Plugin) isCommunityTeam(teamID string) bool {
	if teamID == "" {
		return false
	}
	return p.communityTeams([]string{teamID})[teamID]
}

// callsDefaultForChannel is the Calls setting of a channel that has none of its own: off (false) in the channels of
// communities, nil (the server's default, see userCanStartOrJoin) in corporate ones and elsewhere.
func (p *Plugin) callsDefaultForChannel(channel *model.Channel) *bool {
	if channel == nil || channel.TeamId == "" {
		return nil
	}
	if p.isCommunityTeam(channel.TeamId) {
		return model.NewPointer(false)
	}
	return nil
}

// callsEnabledInChannel is whether calls are enabled in a channel: its own setting, else its community's default
// (nil for the server's default).
func (p *Plugin) callsEnabledInChannel(callsChannel *public.CallsChannel, channel *model.Channel) *bool {
	if callsChannel != nil {
		return model.NewPointer(callsChannel.Enabled)
	}
	return p.callsDefaultForChannel(channel)
}

// defaultDisabledChannels lists, as disabled, the channels of the user's communities (not corporate) that have no Calls
// setting of their own, for the clients that read the channels' settings (GET /channels). Clients don't know the
// kind of communities: to them, these channels have calls turned off.
func (p *Plugin) defaultDisabledChannels(userID string, stored []*public.CallsChannel) []*public.CallsChannel {
	teams, appErr := p.API.GetTeamsForUser(userID)
	if appErr != nil {
		p.LogError("failed to get the user's communities", "userID", userID, "err", appErr.Error())
		return nil
	}
	teamIDs := make([]string, 0, len(teams))
	for _, team := range teams {
		if team != nil && team.DeleteAt == 0 {
			teamIDs = append(teamIDs, team.Id)
		}
	}

	communities := p.communityTeams(teamIDs)

	var hasSetting map[string]bool
	var result []*public.CallsChannel
	for _, teamID := range teamIDs {
		if !communities[teamID] {
			continue
		}
		if hasSetting == nil {
			hasSetting = make(map[string]bool, len(stored))
			for _, ch := range stored {
				hasSetting[ch.ChannelID] = true
			}
		}
		channels, appErr := p.API.GetChannelsForTeamForUser(teamID, userID, false)
		if appErr != nil {
			p.LogError("failed to get the user's channels", "userID", userID, "teamID", teamID, "err", appErr.Error())
			continue
		}
		for _, channel := range channels {
			if channel == nil || hasSetting[channel.Id] {
				continue
			}
			hasSetting[channel.Id] = true
			result = append(result, &public.CallsChannel{ChannelID: channel.Id, Enabled: false})
		}
	}
	return result
}
