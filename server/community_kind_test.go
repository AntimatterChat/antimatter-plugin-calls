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

// setTeamKinds caches the kind of communities, as read from the server's settings: true for a public community
// (everyone in every channel), false for a company's.
func setTeamKinds(p *Plugin, kinds map[string]bool) {
	for id, allChannels := range kinds {
		p.teamKinds.set(id, allChannels, time.Now())
	}
}

func TestCallsDefaultForChannel(t *testing.T) {
	publicTeam, companyTeam := model.NewId(), model.NewId()

	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: &pluginMocks.MockAPI{}}}
	setTeamKinds(p, map[string]bool{publicTeam: true, companyTeam: false})

	publicChannel := &model.Channel{Id: model.NewId(), TeamId: publicTeam, Type: model.ChannelTypeOpen}
	companyChannel := &model.Channel{Id: model.NewId(), TeamId: companyTeam, Type: model.ChannelTypeOpen}
	dm := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeDirect}
	gm := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeGroup}

	t.Run("public communities have calls off", func(t *testing.T) {
		require.Equal(t, model.NewPointer(false), p.callsDefaultForChannel(publicChannel))
	})

	t.Run("a company's communities, direct and group messages follow the server's default", func(t *testing.T) {
		require.Nil(t, p.callsDefaultForChannel(companyChannel))
		require.Nil(t, p.callsDefaultForChannel(dm))
		require.Nil(t, p.callsDefaultForChannel(gm))
		require.Nil(t, p.callsDefaultForChannel(nil))
	})

	t.Run("a channel's own setting wins", func(t *testing.T) {
		require.Equal(t, model.NewPointer(true), p.callsEnabledInChannel(&public.CallsChannel{ChannelID: publicChannel.Id, Enabled: true}, publicChannel))
		require.Equal(t, model.NewPointer(false), p.callsEnabledInChannel(&public.CallsChannel{ChannelID: companyChannel.Id, Enabled: false}, companyChannel))
		require.Equal(t, model.NewPointer(false), p.callsEnabledInChannel(nil, publicChannel))
		require.Nil(t, p.callsEnabledInChannel(nil, companyChannel))
	})

	t.Run("a community whose kind can't be read follows the server's default", func(t *testing.T) {
		unknown := &model.Channel{Id: model.NewId(), TeamId: model.NewId(), Type: model.ChannelTypeOpen}
		require.Nil(t, p.callsDefaultForChannel(unknown))
	})
}

// Starting a call: who may, by the channel's setting, its community's kind and test mode.
func TestUserCanStartOrJoinByCommunityKind(t *testing.T) {
	publicTeam, companyTeam := model.NewId(), model.NewId()
	const admin, member = "admin", "member"

	mockAPI := &pluginMocks.MockAPI{}
	mockAPI.On("HasPermissionTo", admin, model.PermissionManageSystem).Return(true)
	mockAPI.On("HasPermissionTo", member, model.PermissionManageSystem).Return(false)

	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: mockAPI}}
	setTeamKinds(p, map[string]bool{publicTeam: true, companyTeam: false})

	publicChannel := &model.Channel{Id: model.NewId(), TeamId: publicTeam, Type: model.ChannelTypeOpen}
	companyChannel := &model.Channel{Id: model.NewId(), TeamId: companyTeam, Type: model.ChannelTypeOpen}
	dm := &model.Channel{Id: model.NewId(), Type: model.ChannelTypeDirect}

	canStart := func(userID string, stored *public.CallsChannel, channel *model.Channel) error {
		return p.userCanStartOrJoin(userID, p.callsEnabledInChannel(stored, channel))
	}

	t.Run("with the defaults (test mode off)", func(t *testing.T) {
		p.configuration = nil
		require.NoError(t, canStart(member, nil, companyChannel), "members call in a company's channels")
		require.NoError(t, canStart(member, nil, dm), "members call in direct messages")
		require.EqualError(t, canStart(member, nil, publicChannel), "calls are disabled in the channel")
		require.EqualError(t, canStart(admin, nil, publicChannel), "calls are disabled in the channel")

		// Enabled in the channel by its admins.
		require.NoError(t, canStart(member, &public.CallsChannel{ChannelID: publicChannel.Id, Enabled: true}, publicChannel))

		// Disabled in a company's channel.
		require.EqualError(t, canStart(member, &public.CallsChannel{ChannelID: companyChannel.Id, Enabled: false}, companyChannel), "calls are disabled in the channel")
	})

	t.Run("in test mode, only system admins start calls where they aren't enabled", func(t *testing.T) {
		p.configuration = &configuration{ClientConfig: ClientConfig{DefaultEnabled: model.NewPointer(false)}}
		require.EqualError(t, canStart(member, nil, companyChannel), "insufficient permissions")
		require.NoError(t, canStart(admin, nil, companyChannel))
		require.EqualError(t, canStart(admin, nil, publicChannel), "calls are disabled in the channel")
		require.NoError(t, canStart(member, &public.CallsChannel{ChannelID: publicChannel.Id, Enabled: true}, publicChannel))
	})
}

func TestDefaultDisabledChannels(t *testing.T) {
	publicTeam, companyTeam, archivedTeam := model.NewId(), model.NewId(), model.NewId()
	userID := model.NewId()
	plain, enabled, other := model.NewId(), model.NewId(), model.NewId()

	mockAPI := &pluginMocks.MockAPI{}
	defer mockAPI.AssertExpectations(t)
	mockAPI.On("GetTeamsForUser", userID).Return([]*model.Team{
		{Id: publicTeam},
		{Id: companyTeam},
		{Id: archivedTeam, DeleteAt: 1},
	}, nil).Once()
	mockAPI.On("GetChannelsForTeamForUser", publicTeam, userID, false).Return([]*model.Channel{
		{Id: plain, TeamId: publicTeam},
		{Id: enabled, TeamId: publicTeam},
	}, nil).Once()

	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: mockAPI}}
	setTeamKinds(p, map[string]bool{publicTeam: true, companyTeam: false, archivedTeam: true})

	stored := []*public.CallsChannel{{ChannelID: enabled, Enabled: true}, {ChannelID: other, Enabled: false}}
	require.Equal(t, []*public.CallsChannel{{ChannelID: plain, Enabled: false}}, p.defaultDisabledChannels(userID, stored))
}

// The clients learn the channels with calls off from GET /channels and GET /{channel_id}, from the community's
// setting as the Antimatter server stores it.
func TestHandleChannelStatesByCommunityKind(t *testing.T) {
	p, mockAPI, _ := newAPITestPlugin(t)

	publicTeam, companyTeam := model.NewId(), model.NewId()
	userID := model.NewId()
	publicChannel, enabledChannel, companyChannel, dmChannel := model.NewId(), model.NewId(), model.NewId(), model.NewId()

	_, err := p.store.WriterDB().Exec(`INSERT INTO systems (name, value) VALUES ($1, 'true')`, db.TeamAllChannelsKeyPrefix+publicTeam)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := p.store.WriterDB().Exec(`DELETE FROM systems`)
		require.NoError(t, err)
		ResetTestStore(t, p.store)
	})
	require.NoError(t, p.store.CreateCallsChannel(&public.CallsChannel{ChannelID: enabledChannel, Enabled: true}))

	channels := map[string]*model.Channel{
		publicChannel:  {Id: publicChannel, TeamId: publicTeam, Type: model.ChannelTypeOpen},
		enabledChannel: {Id: enabledChannel, TeamId: publicTeam, Type: model.ChannelTypeOpen},
		companyChannel: {Id: companyChannel, TeamId: companyTeam, Type: model.ChannelTypeOpen},
		dmChannel:      {Id: dmChannel, Type: model.ChannelTypeDirect},
	}
	var members []*model.ChannelMember
	for id, ch := range channels {
		mockAPI.On("GetChannel", id).Return(ch, nil)
		mockAPI.On("HasPermissionToChannel", userID, id, model.PermissionReadChannel).Return(true)
		members = append(members, &model.ChannelMember{ChannelId: id, UserId: userID, Roles: "channel_user"})
	}
	mockAPI.On("GetChannelMembersForUser", "", userID, 0, 200).Return(members, nil)
	mockAPI.On("RolesGrantPermission", mock.Anything, model.PermissionReadChannel.Id).Return(true)
	mockAPI.On("GetTeamsForUser", userID).Return([]*model.Team{{Id: publicTeam}, {Id: companyTeam}}, nil)
	mockAPI.On("GetChannelsForTeamForUser", publicTeam, userID, false).Return([]*model.Channel{channels[publicChannel], channels[enabledChannel]}, nil)

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

	t.Run("GET /channels lists the public community's channels without a setting as disabled", func(t *testing.T) {
		var data []map[string]any
		require.NoError(t, json.Unmarshal(get("/channels").Body.Bytes(), &data))
		enabledByID := map[string]any{}
		for _, ch := range data {
			enabledByID[ch["channel_id"].(string)] = ch["enabled"]
		}
		require.Equal(t, map[string]any{publicChannel: false, enabledChannel: true}, enabledByID)
	})

	t.Run("GET /{channel_id} tells the community's default from the server's", func(t *testing.T) {
		var data map[string]any

		require.NoError(t, json.Unmarshal(get(fmt.Sprintf("/%s", publicChannel)).Body.Bytes(), &data))
		require.Equal(t, false, data["enabled"])
		require.NotContains(t, data, "server_default")

		data = nil
		require.NoError(t, json.Unmarshal(get(fmt.Sprintf("/%s", enabledChannel)).Body.Bytes(), &data))
		require.Equal(t, true, data["enabled"])
		require.NotContains(t, data, "server_default")

		for _, id := range []string{companyChannel, dmChannel} {
			data = nil
			require.NoError(t, json.Unmarshal(get(fmt.Sprintf("/%s", id)).Body.Bytes(), &data))
			require.Equal(t, true, data["enabled"], "test mode is off by default")
			require.Equal(t, true, data["server_default"])
		}
	})
}
