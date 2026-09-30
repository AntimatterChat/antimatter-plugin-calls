// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/stretchr/testify/require"
)

func TestInterPluginCallerID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	require.Empty(t, interPluginCallerID(r))

	r.Header.Set("Mattermost-Plugin-ID", "com.example.plugin")
	require.Equal(t, "com.example.plugin", interPluginCallerID(r))

	// A user request is never an inter-plugin request.
	r.Header.Set("Mattermost-User-Id", model.NewId())
	require.Empty(t, interPluginCallerID(r))
}

func TestAPIRouterInterPluginAuth(t *testing.T) {
	p := &Plugin{}
	router := p.newAPIRouter()

	serve := func(method, path string, headers map[string]string) int {
		r := httptest.NewRequest(method, path, nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}

	fromPlugin := map[string]string{"Mattermost-Plugin-ID": "com.example.plugin"}

	t.Run("other plugins can't use other routes", func(t *testing.T) {
		require.Equal(t, http.StatusUnauthorized, serve(http.MethodGet, "/config", fromPlugin))
		require.Equal(t, http.StatusUnauthorized, serve(http.MethodGet, "/channels", fromPlugin))
		require.Equal(t, http.StatusUnauthorized, serve(http.MethodPost, "/calls/"+model.NewId()+"/host/mute", fromPlugin))
	})

	t.Run("anonymous requests are still rejected on the channel settings routes", func(t *testing.T) {
		require.Equal(t, http.StatusUnauthorized, serve(http.MethodGet, "/"+model.NewId(), nil))
		require.Equal(t, http.StatusUnauthorized, serve(http.MethodPost, "/"+model.NewId(), nil))
	})
}
