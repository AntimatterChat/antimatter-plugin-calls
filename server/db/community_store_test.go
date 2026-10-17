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
		"TestGetCorporateTeams": testGetCorporateTeams,
	})
}

func testGetCorporateTeams(t *testing.T, store *Store) {
	corporate, community, unknown, unset, other := model.NewId(), model.NewId(), model.NewId(), model.NewId(), model.NewId()

	// As the Antimatter server keeps them: a row only for a kind that isn't the default (community).
	_, err := store.wDB.Exec(`INSERT INTO systems (name, value) VALUES ($1, 'corporate'), ($2, 'community'), ($3, 'Corporate'), ($4, 'corporate')`,
		TeamKindKeyPrefix+corporate, TeamKindKeyPrefix+community, TeamKindKeyPrefix+unknown, "SomethingElse_"+other)
	require.NoError(t, err)

	t.Run("none asked", func(t *testing.T) {
		got, err := store.GetCorporateTeams(nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("tells each community", func(t *testing.T) {
		// Any other value, or no row, is a community.
		got, err := store.GetCorporateTeams([]string{corporate, community, unknown, unset, other})
		require.NoError(t, err)
		require.Equal(t, map[string]bool{corporate: true, community: false, unknown: false, unset: false, other: false}, got)
	})
}
