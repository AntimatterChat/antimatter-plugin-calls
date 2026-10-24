// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/mattermost/mattermost/server/public/model"
)

func (p *Plugin) handleMakeHost(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleMakeHost", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		NewHostID string `json:"new_host_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.changeHost(userID, callID, payload.NewHostID); err != nil {
		p.handleHostControlsError(err, &res, "handleMakeHost")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleMuteSession(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleMuteSession", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.muteSession(userID, callID, payload.SessionID); err != nil {
		p.handleHostControlsError(err, &res, "handleMuteSession")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleMuteOthers(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleMuteOthers", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	if err := p.muteOthers(userID, callID); err != nil {
		p.handleHostControlsError(err, &res, "handleMuteOthers")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleScreenOff(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleScreenOff", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.screenOff(userID, callID, payload.SessionID); err != nil {
		p.handleHostControlsError(err, &res, "handleScreenOff")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleLowerHand(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleLowerHand", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.lowerHand(userID, callID, payload.SessionID); err != nil {
		p.handleHostControlsError(err, &res, "handleLowerHand")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleRemoveSession(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleRemoveSession", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.hostRemoveSession(userID, callID, payload.SessionID); err != nil {
		p.handleHostControlsError(err, &res, "handleRemoveSession")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleEnd(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleEnd", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	if err := p.hostEnd(userID, callID); err != nil {
		p.handleHostControlsError(err, &res, "handleEnd")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleModMute(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleModMute", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		UserID string `json:"user_id"`
		Muted  bool   `json:"muted"`
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

	if err := p.setModMuted(userID, callID, payload.UserID, payload.Muted); err != nil {
		p.handleModerationError(err, &res, "handleModMute")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleAskUnmute(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleAskUnmute", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.askToUnmute(userID, callID, payload.SessionID); err != nil {
		p.handleModerationError(err, &res, "handleAskUnmute")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleModDeafen(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleModDeafen", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		UserID   string `json:"user_id"`
		Deafened bool   `json:"deafened"`
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

	if err := p.setModDeafened(userID, callID, payload.UserID, payload.Deafened); err != nil {
		p.handleModerationError(err, &res, "handleModDeafen")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleMoveSession(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleMoveSession", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	callID := mux.Vars(r)["call_id"]

	var payload struct {
		SessionID string `json:"session_id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyMaxSizeBytes)).Decode(&payload); err != nil {
		res.Err = err.Error()
		res.Code = http.StatusBadRequest
		return
	}
	if !model.IsValidId(payload.SessionID) || !model.IsValidId(payload.ChannelID) {
		res.Err = "invalid session_id or channel_id"
		res.Code = http.StatusBadRequest
		return
	}

	if err := p.moveSession(userID, callID, payload.SessionID, payload.ChannelID); err != nil {
		p.handleModerationError(err, &res, "handleMoveSession")
		return
	}

	res.Code = http.StatusOK
	res.Msg = "success"
}

func (p *Plugin) handleListModMutes(w http.ResponseWriter, r *http.Request) {
	var res httpResponse
	defer p.httpAudit("handleListModMutes", &res, w, r)

	userID := r.Header.Get("Mattermost-User-Id")
	channelID := mux.Vars(r)["call_id"]

	list, err := p.listModMutes(userID, channelID)
	if err != nil {
		p.handleModerationError(err, &res, "handleListModMutes")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(list); err != nil {
		p.LogError("failed to encode moderator mutes", "err", err.Error())
	}
}

// handleModerationError is handleHostControlsError for the moderation controls, which tell refusals (403) and
// rate limits (429) apart.
func (p *Plugin) handleModerationError(err error, res *httpResponse, handlerName string) {
	switch {
	case errors.Is(err, ErrNoPermissions), errors.Is(err, ErrOutranked):
		res.Code = http.StatusForbidden
	case errors.Is(err, ErrRateLimited):
		res.Code = http.StatusTooManyRequests
	case errors.Is(err, ErrNoCallOngoing), errors.Is(err, ErrNotInCall), errors.Is(err, ErrNotAllowed):
		res.Code = http.StatusBadRequest
	default:
		p.LogError(handlerName, "err", err.Error())
		res.Code = http.StatusInternalServerError
	}
	res.Err = err.Error()
}

func (p *Plugin) handleHostControlsError(err error, res *httpResponse, handlerName string) {
	p.LogError(handlerName, "err", err.Error())

	res.Code = http.StatusInternalServerError
	if errors.Is(err, ErrNoCallOngoing) ||
		errors.Is(err, ErrNoPermissions) ||
		errors.Is(err, ErrNotInCall) ||
		errors.Is(err, ErrNotAllowed) {
		res.Code = http.StatusBadRequest
	}

	res.Err = err.Error()
}
