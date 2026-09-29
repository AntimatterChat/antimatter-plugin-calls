// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	maxAdminsToQueryForNotification = 25

	// The value of concurrent sessions (globally) that will trigger a warning if the plugin is not using
	// a dedicated rtcd service.
	concurrentSessionsThresholdDefault          = 50
	concurrentSessionsWarningBackoffTimeDefault = 24 * 7 * time.Hour // 1 week
)

func getConcurrentSessionsThreshold() int64 {
	val, err := strconv.Atoi(os.Getenv("MM_CALLS_CONCURRENT_SESSIONS_THRESHOLD"))
	if err != nil {
		return int64(concurrentSessionsThresholdDefault)
	}
	return int64(val)
}

func getConcurrentSessionsWarningBackoffTime() time.Duration {
	val, err := time.ParseDuration(os.Getenv("MM_CALLS_CONCURRENT_SESSIONS_WARNING_BACKOFF_TIME"))
	if err != nil {
		return concurrentSessionsWarningBackoffTimeDefault
	}
	return val
}

func (p *Plugin) shouldSendConcurrentSessionsWarning(threshold int64, backoff time.Duration) (bool, error) {
	// Nothing to do if rtcd is being used.
	if p.rtcdManager != nil {
		return false, nil
	}

	// Get the global number of active call sessions.
	count, err := p.store.GetTotalActiveSessions()
	if err != nil {
		return false, fmt.Errorf("failed to get total active sessions: %w", err)
	}

	// We return early if the value is not at or above threshold.
	if count < threshold {
		return false, nil
	}

	// We use the native ExpireAt functionality on KV store to implement a simple backoff mechanism.
	// If this is the first insert or the time has expired we'll be able to perform the set operation.
	// This also ensures only one node will be sending the warning at any given time.
	ok, appErr := p.API.KVSetWithOptions("concurrent_sessions_warning", []byte{1}, model.PluginKVSetOptions{
		Atomic:          true,
		OldValue:        nil,
		ExpireInSeconds: int64(backoff.Seconds()),
	})
	if appErr != nil {
		return false, fmt.Errorf("failed to set kv: %w", appErr)
	}

	if ok {
		return true, nil
	}

	return false, nil
}

func (p *Plugin) sendConcurrentSessionsWarning() error {
	p.LogWarn("The number of active call sessions is high. Consider deploying a dedicated RTCD service.")

	admins, appErr := p.API.GetUsers(&model.UserGetOptions{
		Role:    model.SystemAdminRoleId,
		Page:    0,
		PerPage: maxAdminsToQueryForNotification,
	})
	if appErr != nil {
		return fmt.Errorf("failed to get admin users: %w", appErr)
	} else if len(admins) == 0 {
		return fmt.Errorf("no admin user found")
	}

	botID := p.getBotID()

	for _, admin := range admins {
		dm, appErr := p.API.GetDirectChannel(admin.Id, botID)
		if appErr != nil {
			p.LogError("failed to get dm between admin and bot",
				"userID", admin.Id, "botID", botID, "err", appErr.Error())
			continue
		}

		T := p.getTranslationFunc(admin.Locale)

		msg := T("app.admin.concurrent_sessions_warning.intro")
		msg += "\r\n\r\n"
		msg += T("app.admin.concurrent_sessions_warning.rtcd")

		post := &model.Post{
			Message:   ":warning: " + msg,
			UserId:    botID,
			ChannelId: dm.Id,
		}

		if _, appErr := p.API.CreatePost(post); appErr != nil {
			p.LogError("failed to create warning post",
				"userID", admin.Id, "botID", botID, "err", appErr.Error())
		}
	}

	return nil
}
