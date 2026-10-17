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
// (Team.Kind): "workplace" for a company's community, "community" for the others. The server writes it for every
// community; a missing row, or any other value, is a community. The server's model.Team has the field, but the
// plugin API's copy of it doesn't, so it is read from there.
const TeamKindKeyPrefix = "AmTeamKind_"

// TeamKindWorkplace is the kind of a company's community.
const TeamKindWorkplace = "workplace"

// GetWorkplaceTeams tells which of the given communities are a company's (workplace kind); the others are
// communities.
func (s *Store) GetWorkplaceTeams(teamIDs []string) (map[string]bool, error) {
	s.metrics.IncStoreOp("GetWorkplaceTeams")
	defer func(start time.Time) {
		s.metrics.ObserveStoreMethodsTime("GetWorkplaceTeams", time.Since(start).Seconds())
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
		Where(sq.Eq{"Value": TeamKindWorkplace})
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
