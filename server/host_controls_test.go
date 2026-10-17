// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"testing"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/public"

	pluginMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/mattermost/mattermost/server/public/plugin"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Antimatter: ending a call for everyone, from the call's card once out of it.
func TestCanEndCall(t *testing.T) {
	teamID := model.NewId()
	channelID := model.NewId()
	voiceID := model.NewId()

	const (
		sysadmin  = "sysadmin"
		teamAdmin = "teamadmin"
		chanAdmin = "chanadmin"
		moderator = "moderator"
		host      = "host"
		starter   = "starter"
		regular   = "regular"
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

	mockAPI.On("GetChannel", channelID).Return(&model.Channel{Id: channelID, TeamId: teamID, Type: model.ChannelTypeOpen}, nil)
	mockAPI.On("GetChannel", voiceID).Return(&model.Channel{Id: voiceID, TeamId: teamID, Type: model.ChannelTypeOpen}, nil)
	mockAPI.On("HasPermissionTo", sysadmin, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", mock.Anything, model.PermissionManageSystem).Return(false)
	mockAPI.On("HasPermissionToTeam", teamAdmin, teamID, model.PermissionManageTeam).Return(true)
	mockAPI.On("HasPermissionToChannel", chanAdmin, mock.Anything, model.PermissionManageChannelRoles).Return(true)
	mockAPI.On("HasPermissionToTeam", moderator, teamID, model.PermissionRemoveUserFromTeam).Return(true)
	mockAPI.On("HasPermissionToTeam", mock.Anything, mock.Anything, mock.Anything).Return(false)
	mockAPI.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(false)

	// The starter left: the host passed to someone else.
	state := &callState{Call: public.Call{OwnerID: starter, Props: public.CallProps{Hosts: []string{host}}}}

	tcs := []struct {
		user           string
		channel, voice bool
	}{
		{sysadmin, true, true},
		{teamAdmin, true, true},
		{chanAdmin, true, true},
		{moderator, false, true},
		{host, true, true},
		{starter, true, false},
		{regular, false, false},
		{"", false, false},
	}
	for _, tc := range tcs {
		t.Run(tc.user, func(t *testing.T) {
			require.Equal(t, tc.channel, p.canEndCall(tc.user, channelID, state), "in a channel")
			require.Equal(t, tc.voice, p.canEndCall(tc.user, voiceID, state), "in a voice channel")
		})
	}

	t.Run("no call", func(t *testing.T) {
		require.False(t, p.canEndCall(sysadmin, channelID, nil))
	})
}
