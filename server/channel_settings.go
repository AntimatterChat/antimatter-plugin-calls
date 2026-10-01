// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"errors"
	"sync"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/db"
	"github.com/antimatterchat/antimatter-plugin-calls/server/public"
)

// channelSettingsCacheTTL is how long the Calls settings of a channel are cached for the
// decisions taken on frequent events (e.g. who receives voice activity events). A change made
// through another node of a cluster applies after at most this long.
const channelSettingsCacheTTL = 30 * time.Second

// channelSettingsCacheMaxEntries is the size above which expired entries are dropped.
const channelSettingsCacheMaxEntries = 1000

type channelSettingsCacheEntry struct {
	channel   *public.CallsChannel
	expiresAt time.Time
}

// channelSettingsCache caches the Calls settings of channels. Its zero value is ready to use.
type channelSettingsCache struct {
	mut     sync.Mutex
	entries map[string]channelSettingsCacheEntry
}

// get returns the settings of the channel, calling fetch when they aren't cached or have
// expired. A nil channel (no settings stored) is cached too.
func (c *channelSettingsCache) get(channelID string, now time.Time, fetch func(channelID string) (*public.CallsChannel, error)) (*public.CallsChannel, error) {
	c.mut.Lock()
	entry, ok := c.entries[channelID]
	c.mut.Unlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.channel, nil
	}

	channel, err := fetch(channelID)
	if err != nil {
		return nil, err
	}

	c.mut.Lock()
	defer c.mut.Unlock()
	if c.entries == nil {
		c.entries = map[string]channelSettingsCacheEntry{}
	}
	if len(c.entries) >= channelSettingsCacheMaxEntries {
		for id, e := range c.entries {
			if !now.Before(e.expiresAt) {
				delete(c.entries, id)
			}
		}
	}
	c.entries[channelID] = channelSettingsCacheEntry{channel: channel, expiresAt: now.Add(channelSettingsCacheTTL)}

	return channel, nil
}

// invalidate drops the cached settings of the channel.
func (c *channelSettingsCache) invalidate(channelID string) {
	c.mut.Lock()
	defer c.mut.Unlock()
	delete(c.entries, channelID)
}

// getCachedCallsChannel returns the Calls settings of the channel (nil if it has none),
// possibly cached for up to channelSettingsCacheTTL.
func (p *Plugin) getCachedCallsChannel(channelID string) *public.CallsChannel {
	channel, err := p.channelSettings.get(channelID, time.Now(), func(channelID string) (*public.CallsChannel, error) {
		if p.store == nil {
			return nil, nil
		}
		channel, err := p.store.GetCallsChannel(channelID, db.GetCallsChannelOpts{})
		if errors.Is(err, db.ErrNotFound) {
			return nil, nil
		}
		return channel, err
	})
	if err != nil {
		p.LogError("failed to get calls channel", "channelID", channelID, "err", err.Error())
		return nil
	}

	return channel
}

// sessionStateBroadcast returns the broadcast of an event about the state of a session in a
// call of the given channel: the call participants, or every channel member if the channel
// has ChannelPropBroadcastSessionState set. getSessions is only called in the former case.
func (p *Plugin) sessionStateBroadcast(channelID string, getSessions func() (map[string]*public.CallSession, error)) (*WebSocketBroadcast, error) {
	if p.getCachedCallsChannel(channelID).BoolProp(public.ChannelPropBroadcastSessionState) {
		return &WebSocketBroadcast{ChannelID: channelID}, nil
	}

	sessions, err := getSessions()
	if err != nil {
		return nil, err
	}

	return &WebSocketBroadcast{ChannelID: channelID, UserIDs: getUserIDsFromSessions(sessions)}, nil
}

// sessionsGetter returns a getSessions function for sessionStateBroadcast.
func sessionsGetter(sessions map[string]*public.CallSession) func() (map[string]*public.CallSession, error) {
	return func() (map[string]*public.CallSession, error) {
		return sessions, nil
	}
}
