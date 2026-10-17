// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	sq "github.com/mattermost/squirrel"
)

// TeamKindKeyPrefix names the Systems row in which the Antimatter server keeps the kind of a community
// (Team.Kind): "corporate" for a company's community. The row is only written when the kind isn't the default:
// no row, or any other value, is a (public) community. The server's model.Team has the field, but the plugin API's
// copy of it doesn't, so it is read from there.
const TeamKindKeyPrefix = "AmTeamKind_"

// TeamKindCorporate is the kind of a company's community.
const TeamKindCorporate = "corporate"

// GetCorporateTeams tells which of the given communities are a company's (corporate kind); the others are
// communities.
func (s *Store) GetCorporateTeams(teamIDs []string) (map[string]bool, error) {
	s.metrics.IncStoreOp("GetCorporateTeams")
	defer func(start time.Time) {
		s.metrics.ObserveStoreMethodsTime("GetCorporateTeams", time.Since(start).Seconds())
	}(time.Now())

	result := make(map[string]bool, len(teamIDs))
	if len(teamIDs) == 0 {
		return result, nil
	}

	names := make([]string, 0, len(teamIDs))
	for _, id := range teamIDs {
		result[id] = false
		names = append(names, TeamKindKeyPrefix+id)
	}

	qb := getQueryBuilder().
		Select("Name").
		From("Systems").
		Where(sq.Eq{"Name": names}).
		Where(sq.Eq{"Value": TeamKindCorporate})
	q, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare query: %w", err)
	}

	var found []string
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*s.settings.QueryTimeout)*time.Second)
	defer cancel()
	if err := s.rDBx.SelectContext(ctx, &found, q, args...); err != nil {
		return nil, fmt.Errorf("failed to get the kind of communities: %w", err)
	}
	for _, name := range found {
		result[strings.TrimPrefix(name, TeamKindKeyPrefix)] = true
	}

	return result, nil
}
