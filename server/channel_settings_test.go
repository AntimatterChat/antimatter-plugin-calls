// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"errors"
	"testing"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/stretchr/testify/require"
)

func TestChannelSettingsCache(t *testing.T) {
	channelID := model.NewId()
	now := time.Now()

	var calls int
	var channel *public.CallsChannel
	var fetchErr error
	fetch := func(id string) (*public.CallsChannel, error) {
		require.Equal(t, channelID, id)
		calls++
		return channel, fetchErr
	}

	var c channelSettingsCache

	t.Run("caches missing settings", func(t *testing.T) {
		got, err := c.get(channelID, now, fetch)
		require.NoError(t, err)
		require.Nil(t, got)
		got, err = c.get(channelID, now.Add(time.Second), fetch)
		require.NoError(t, err)
		require.Nil(t, got)
		require.Equal(t, 1, calls)
	})

	t.Run("refetches after expiry", func(t *testing.T) {
		channel = &public.CallsChannel{ChannelID: channelID, Enabled: true}
		got, err := c.get(channelID, now.Add(channelSettingsCacheTTL), fetch)
		require.NoError(t, err)
		require.Equal(t, channel, got)
		require.Equal(t, 2, calls)
	})

	t.Run("refetches after invalidation", func(t *testing.T) {
		c.invalidate(channelID)
		_, err := c.get(channelID, now.Add(channelSettingsCacheTTL), fetch)
		require.NoError(t, err)
		require.Equal(t, 3, calls)
	})

	t.Run("doesn't cache errors", func(t *testing.T) {
		c.invalidate(channelID)
		fetchErr = errors.New("db error")
		_, err := c.get(channelID, now, fetch)
		require.Error(t, err)
		fetchErr = nil
		_, err = c.get(channelID, now, fetch)
		require.NoError(t, err)
		require.Equal(t, 5, calls)
	})
}

func TestSessionStateBroadcast(t *testing.T) {
	userID := model.NewId()
	sessions := map[string]*public.CallSession{
		"sessionA": {ID: "sessionA", UserID: userID},
	}

	cacheSettings := func(p *Plugin, channel *public.CallsChannel) {
		p.channelSettings.entries = map[string]channelSettingsCacheEntry{
			channel.ChannelID: {channel: channel, expiresAt: time.Now().Add(time.Hour)},
		}
	}

	t.Run("participants by default", func(t *testing.T) {
		p := &Plugin{}
		channelID := model.NewId()
		cacheSettings(p, &public.CallsChannel{ChannelID: channelID, Enabled: true})

		bc, err := p.sessionStateBroadcast(channelID, sessionsGetter(sessions))
		require.NoError(t, err)
		require.Equal(t, &WebSocketBroadcast{ChannelID: channelID, UserIDs: []string{userID}}, bc)
	})

	t.Run("whole channel when enabled", func(t *testing.T) {
		p := &Plugin{}
		channelID := model.NewId()
		cacheSettings(p, &public.CallsChannel{
			ChannelID: channelID,
			Enabled:   true,
			Props:     public.StringMap{public.ChannelPropBroadcastSessionState: true},
		})

		bc, err := p.sessionStateBroadcast(channelID, func() (map[string]*public.CallSession, error) {
			require.FailNow(t, "sessions should not be fetched")
			return nil, nil
		})
		require.NoError(t, err)
		require.Equal(t, &WebSocketBroadcast{ChannelID: channelID}, bc)
	})

	t.Run("sessions error", func(t *testing.T) {
		p := &Plugin{}
		_, err := p.sessionStateBroadcast(model.NewId(), func() (map[string]*public.CallSession, error) {
			return nil, errors.New("db error")
		})
		require.Error(t, err)
	})
}
