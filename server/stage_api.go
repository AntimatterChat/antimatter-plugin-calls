// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/mattermost/mattermost/server/public/model"
)

// handleInviteToSpeak invites someone of a stage's audience (user_id) to speak, for those who may moderate them.
func (p *Plugin) handleInviteToSpeak(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleInviteToSpeak", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	channelID := mux.Vars(r)["call_id"]

	var payload struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}
	if !model.IsValidId(payload.UserID) {
		res.Err = "invalid user_id"
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.inviteToSpeak(userID, channelID, payload.UserID); err != nil {
		p.handleModerationError(err, &res, "handleInviteToSpeak")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

// handleMoveToAudience moves an invited speaker of a stage back to its audience: by those who may moderate them, or
// by the speaker themselves.
func (p *Plugin) handleMoveToAudience(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleMoveToAudience", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	vars := mux.Vars(r)

	if err := p.moveToAudience(userID, vars["call_id"], vars["user_id"]); err != nil {
		p.handleModerationError(err, &res, "handleMoveToAudience")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}
