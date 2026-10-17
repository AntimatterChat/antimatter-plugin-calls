// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package db

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/stretchr/testify/require"
)

func TestCommunityStore(t *testing.T) {
	testStore(t, map[string]func(t *testing.T, store *Store){
		"TestGetAllChannelsTeams": testGetAllChannelsTeams,
	})
}

func testGetAllChannelsTeams(t *testing.T, store *Store) {
	everyone, company, other := model.NewId(), model.NewId(), model.NewId()

	// As the Antimatter server keeps them: a row with "true" for each community with everyone in every channel.
	_, err := store.wDB.Exec(`INSERT INTO systems (name, value) VALUES ($1, 'true'), ($2, 'false'), ($3, 'true')`,
		TeamAllChannelsKeyPrefix+everyone, TeamAllChannelsKeyPrefix+company, "SomethingElse_"+other)
	require.NoError(t, err)

	t.Run("none asked", func(t *testing.T) {
		got, err := store.GetAllChannelsTeams(nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tells each community", func(t *testing.T) {
		got, err := store.GetAllChannelsTeams([]string{everyone, company, other})
		require.NoError(t, err)
		require.Equal(t, map[string]bool{everyone: true, company: false, other: false}, got)
	})
}
