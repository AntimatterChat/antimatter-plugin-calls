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

// TeamAllChannelsKeyPrefix names the Systems row in which the Antimatter server keeps a community's "Members are in
// every channel" setting (Team.AllChannelsMembership), with the value "true", for each community that has it on.
// The server's model.Team has the field, but the plugin API's copy of it doesn't, so it is read from there.
const TeamAllChannelsKeyPrefix = "AmTeamAllChannels_"

// GetAllChannelsTeams tells which of the given communities have everyone in every channel, as Discord-like
// public communities do (a company's communities have people join the channels of their work).
func (s *Store) GetAllChannelsTeams(teamIDs []string) (map[string]bool, error) {
	s.metrics.IncStoreOp("GetAllChannelsTeams")
	defer func(start time.Time) {
		s.metrics.ObserveStoreMethodsTime("GetAllChannelsTeams", time.Since(start).Seconds())
	}(time.Now())

	result := make(map[string]bool, len(teamIDs))
	if len(teamIDs) == 0 {
		return result, nil
	}

	names := make([]string, 0, len(teamIDs))
	for _, id := range teamIDs {
		result[id] = false
		names = append(names, TeamAllChannelsKeyPrefix+id)
	}

	qb := getQueryBuilder().
		Select("Name").
		From("Systems").
		Where(sq.Eq{"Name": names}).
		Where(sq.Eq{"Value": "true"})
	q, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare query: %w", err)
	}

	var found []string
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*s.settings.QueryTimeout)*time.Second)
	defer cancel()
	if err := s.rDBx.SelectContext(ctx, &found, q, args...); err != nil {
		return nil, fmt.Errorf("failed to get community settings: %w", err)
	}
	for _, name := range found {
		result[strings.TrimPrefix(name, TeamAllChannelsKeyPrefix)] = true
	}

	return result, nil
}
