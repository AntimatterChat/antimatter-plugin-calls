// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package performance

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func familyNames(families []*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for _, f := range families {
		names = append(names, f.GetName())
	}
	return names
}

func TestWithAntimatterNames(t *testing.T) {
	t.Run("copies mattermost families only, sorted", func(t *testing.T) {
		in := []*dto.MetricFamily{
			{Name: new("go_goroutines")},
			{Name: new("mattermost_plugin_calls_websocket_connections_total"), Help: new("Conns"), Type: dto.MetricType_GAUGE.Enum(), Metric: []*dto.Metric{
				{Gauge: &dto.Gauge{Value: new(2.0)}},
			}},
		}
		out := withAntimatterNames(in)

		require.Equal(t, []string{
			"antimatter_plugin_calls_websocket_connections_total",
			"go_goroutines",
			"mattermost_plugin_calls_websocket_connections_total",
		}, familyNames(out))
		assert.Equal(t, "Conns", out[0].GetHelp())
		assert.Equal(t, dto.MetricType_GAUGE, out[0].GetType())
		assert.Equal(t, 2.0, out[0].GetMetric()[0].GetGauge().GetValue())
		assert.Equal(t, []string{"go_goroutines", "mattermost_plugin_calls_websocket_connections_total"}, familyNames(in))
	})

	t.Run("an existing antimatter family wins over the copy", func(t *testing.T) {
		out := withAntimatterNames([]*dto.MetricFamily{
			{Name: new("antimatter_x"), Help: new("native")},
			{Name: new("mattermost_x"), Help: new("legacy")},
		})
		require.Len(t, out, 2)
		assert.Equal(t, "native", out[0].GetHelp())
	})
}

type failingGatherer struct{}

func (failingGatherer) Gather() ([]*dto.MetricFamily, error) {
	return []*dto.MetricFamily{{Name: new("mattermost_a")}}, errors.New("collector failed")
}

func TestAntimatterNamesGathererKeepsPartialResults(t *testing.T) {
	families, err := antimatterNamesGatherer{Gatherer: failingGatherer{}}.Gather()
	require.Error(t, err)
	assert.Equal(t, []string{"antimatter_a", "mattermost_a"}, familyNames(families))
}

func TestMetricsHandlerServesBothPrefixes(t *testing.T) {
	m := NewMetrics()
	m.IncWebSocketConn()
	m.IncStoreOp("GetCall")

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	for _, prefix := range []string{"mattermost_", "antimatter_"} {
		assert.Contains(t, body, prefix+"plugin_calls_websocket_connections_total 1")
		assert.Contains(t, body, prefix+`plugin_calls_store_ops_total{type="GetCall"} 1`)
	}
	assert.NotContains(t, body, "antimatter_go_")
}
