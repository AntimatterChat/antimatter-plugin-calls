// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/antimatterchat/antimatter-plugin-calls/server/db"
	"github.com/antimatterchat/antimatter-plugin-calls/server/public"

	pluginMocks "github.com/antimatterchat/antimatter-plugin-calls/server/mocks/github.com/mattermost/mattermost/server/public/plugin"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setTeamKinds caches the kind of communities, as read from the server's settings: true for a community, false
// for a workplace one.
func setTeamKinds(p *Plugin, kinds map[string]bool) {
	for id, community := range kinds {
		p.teamKinds.set(id, community, time.Now())
	}
}

func TestCallsDefaultForChannel(t *testing.T) {
	communityTeam, workplaceTeam := model.NewId(), model.NewId()

	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: &pluginMocks.MockAPI{}}}
	setTeamKinds(p, map[string]bool{communityTeam: true, workplaceTeam: false})

	communityChannel := &model.Channel{Id: model.NewId(), TeamId: communityTeam, Type: model.ChannelTypeOpen}
	workplaceChannel := &model.Channel{Id: model.NewId(), TeamId: workplaceTeam, Type: model.ChannelTypeOpen}
	dm := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeDirect}
	gm := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeGroup}

	t.Run("communities have calls off", func(t *testing.T) {
		require.Equal(t, model.NewPointer(false), p.callsDefaultForChannel(communityChannel))
	})

	t.Run("workplace communities, direct and group messages follow the server's default", func(t *testing.T) {
		require.Nil(t, p.callsDefaultForChannel(workplaceChannel))
		require.Nil(t, p.callsDefaultForChannel(dm))
		require.Nil(t, p.callsDefaultForChannel(gm))
		require.Nil(t, p.callsDefaultForChannel(nil))
	})

	t.Run("a channel's own setting wins", func(t *testing.T) {
		require.Equal(t, model.NewPointer(true), p.callsEnabledInChannel(&public.CallsChannel{ChannelID: communityChannel.Id, Enabled: true}, communityChannel))
		require.Equal(t, model.NewPointer(false), p.callsEnabledInChannel(&public.CallsChannel{ChannelID: workplaceChannel.Id, Enabled: false}, workplaceChannel))
		require.Equal(t, model.NewPointer(false), p.callsEnabledInChannel(nil, communityChannel))
		require.Nil(t, p.callsEnabledInChannel(nil, workplaceChannel))
	})

	t.Run("a community whose kind can't be read follows the server's default", func(t *testing.T) {
		unknown := &model.Channel{Id: model.NewId(), TeamId: model.NewId(), Type: model.ChannelTypeOpen}
		require.Nil(t, p.callsDefaultForChannel(unknown))
	})
}

// Starting a call: who may, by the channel's setting, its community's kind and test mode.
func TestUserCanStartOrJoinByCommunityKind(t *testing.T) {
	communityTeam, workplaceTeam := model.NewId(), model.NewId()
	const admin, member = "admin", "member"

	mockAPI := &pluginMocks.MockAPI{}
	mockAPI.On("HasPermissionTo", admin, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", member, model.PermissionManageSystem).Return(false)

	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: mockAPI}}
	setTeamKinds(p, map[string]bool{communityTeam: true, workplaceTeam: false})

	communityChannel := &model.Channel{Id: model.NewId(), TeamId: communityTeam, Type: model.ChannelTypeOpen}
	workplaceChannel := &model.Channel{Id: model.NewId(), TeamId: workplaceTeam, Type: model.ChannelTypeOpen}
	dm := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeDirect}

	canStart := func(userID string, stored *public.CallsChannel, channel *model.Channel) error {
		return p.userCanStartOrJoin(userID, p.callsEnabledInChannel(stored, channel))
	}

	t.Run("with the defaults (test mode off)", func(t *testing.T) {
		p.configuration = nil
		require.NoError(t, canStart(member, nil, workplaceChannel), "members call in workplace channels")
		require.NoError(t, canStart(member, nil, dm), "members call in direct messages")
		require.EqualError(t, canStart(member, nil, communityChannel), "calls are disabled in the channel")
		require.EqualError(t, canStart(admin, nil, communityChannel), "calls are disabled in the channel")

		// Enabled in the channel by its admins.
		require.NoError(t, canStart(member, &public.CallsChannel{ChannelID: communityChannel.Id, Enabled: true}, communityChannel))

		// Disabled in a workplace channel.
		require.EqualError(t, canStart(member, &public.CallsChannel{ChannelID: workplaceChannel.Id, Enabled: false}, workplaceChannel), "calls are disabled in the channel")
	})

	t.Run("in test mode, only system admins start calls where they aren't enabled", func(t *testing.T) {
		p.configuration = &configuration{ClientConfig: ClientConfig{DefaultEnabled: model.NewPointer(false)}}
		require.EqualError(t, canStart(member, nil, workplaceChannel), "insufficient permissions")
		require.NoError(t, canStart(admin, nil, workplaceChannel))
		require.EqualError(t, canStart(admin, nil, communityChannel), "calls are disabled in the channel")
		require.NoError(t, canStart(member, &public.CallsChannel{ChannelID: communityChannel.Id, Enabled: true}, communityChannel))
	})
}

func TestDefaultDisabledChannels(t *testing.T) {
	communityTeam, workplaceTeam, archivedTeam := model.NewId(), model.NewId(), model.NewId()
	userID := model.NewId()
	plain, enabled, other := model.NewId(), model.NewId(), model.NewId()

	mockAPI := &pluginMocks.MockAPI{}
	defer mockAPI.AssertExpectations(t)
	mockAPI.On("GetTeamsForUser", userID).Return([]*model.Team{
		{Id: communityTeam},
		{Id: workplaceTeam},
		{Id: archivedTeam, DeleteAt: 1},
	}, nil).Once()
	mockAPI.On("GetChannelsForTeamForUser", communityTeam, userID, false).Return([]*model.Channel{
		{Id: plain, TeamId: communityTeam},
		{Id: enabled, TeamId: communityTeam},
	}, nil).Once()

	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: mockAPI}}
	setTeamKinds(p, map[string]bool{communityTeam: true, workplaceTeam: false, archivedTeam: true})

	stored := []*public.CallsChannel{{ChannelID: enabled, Enabled: true}, {ChannelID: other, Enabled: false}}
	require.Equal(t, []*public.CallsChannel{{ChannelID: plain, Enabled: false}}, p.defaultDisabledChannels(userID, stored))
}

// The clients learn the channels with calls off from GET /channels and GET /{channel_id}, from the community's
// setting as the Antimatter server stores it.
func TestHandleChannelStatesByCommunityKind(t *testing.T) {
	p, mockAPI, _ := newAPITestPlugin(t)

	communityTeam, workplaceTeam := model.NewId(), model.NewId()
	userID := model.NewId()
	communityChannel, enabledChannel, workplaceChannel, dmChannel := model.NewId(), model.NewId(), model.NewId(), model.NewId()

	// The server writes the kind of every community; a community without a row (from before the kinds) is a community.
	_, err := p.store.WriterDB().Exec(`INSERT INTO systems (name, value) VALUES ($1, 'workplace')`, db.TeamKindKeyPrefix+workplaceTeam)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := p.store.WriterDB().Exec(`DELETE FROM systems`)
		require.NoError(t, err)
		ResetTestStore(t, p.store)
	})
	require.NoError(t, p.store.CreateCallsChannel(&public.CallsChannel{ChannelID: enabledChannel, Enabled: true}))

	channels := map[string]*model.Channel{
		communityChannel: {Id: communityChannel, TeamId: communityTeam, Type: model.ChannelTypeOpen},
		enabledChannel:   {Id: enabledChannel, TeamId: communityTeam, Type: model.ChannelTypeOpen},
		workplaceChannel: {Id: workplaceChannel, TeamId: workplaceTeam, Type: model.ChannelTypeOpen},
		dmChannel:        {Id: dmChannel, Type: model.ChannelTypeDirect},
	}
	var members []*model.ChannelMember
	for id, ch := range channels {
		mockAPI.On("GetChannel", id).Return(ch, nil)
		mockAPI.On("HasPermissionToChannel", userID, id, model.PermissionReadChannel).Return(true)
		members = append(members, &model.ChannelMember{ChannelId: id, UserId: userID, Roles: "channel_user"})
	}
	mockAPI.On("GetChannelMembersForUser", "", userID, 0, 200).Return(members, nil)
	mockAPI.On("RolesGrantPermission", mock.Anything, model.PermissionReadChannel.Id).Return(true)
	mockAPI.On("GetTeamsForUser", userID).Return([]*model.Team{{Id: communityTeam}, {Id: workplaceTeam}}, nil)
	mockAPI.On("GetChannelsForTeamForUser", communityTeam, userID, false).Return([]*model.Channel{channels[communityChannel], channels[enabledChannel]}, nil)

	mockMetricsForRouter(t, p)
	router := p.newAPIRouter()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Mattermost-User-Id", userID)
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Result().StatusCode, w.Body.String())
		return w
	}

	t.Run("GET /channels lists the community's channels without a setting as disabled", func(t *testing.T) {
		var data []map[string]any
		require.NoError(t, json.Unmarshal(get("/channels").Body.Bytes(), &data))
		enabledByID := map[string]any{}
		for _, ch := range data {
			enabledByID[ch["channel_id"].(string)] = ch["enabled"]
		}
		require.Equal(t, map[string]any{communityChannel: false, enabledChannel: true}, enabledByID)
	})

	t.Run("GET /{channel_id} tells the community's default from the server's", func(t *testing.T) {
		var data map[string]any

		require.NoError(t, json.Unmarshal(get(fmt.Sprintf("/%s", communityChannel)).Body.Bytes(), &data))
		require.Equal(t, false, data["enabled"])
		require.NotContains(t, data, "server_default")

		data = nil
		require.NoError(t, json.Unmarshal(get(fmt.Sprintf("/%s", enabledChannel)).Body.Bytes(), &data))
		require.Equal(t, true, data["enabled"])
		require.NotContains(t, data, "server_default")

		for _, id := range []string{workplaceChannel, dmChannel} {
			data = nil
			require.NoError(t, json.Unmarshal(get(fmt.Sprintf("/%s", id)).Body.Bytes(), &data))
			require.Equal(t, true, data["enabled"], "test mode is off by default")
			require.Equal(t, true, data["server_default"])
		}
	})
}
