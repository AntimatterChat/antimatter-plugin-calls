// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package public

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallsChannelBoolProp(t *testing.T) {
	t.Run("nil channel", func(t *testing.T) {
		var c *CallsChannel
		require.False(t, c.BoolProp("some_prop"))
	})

	t.Run("no props", func(t *testing.T) {
		c := &CallsChannel{ChannelID: "channelID"}
		require.False(t, c.BoolProp("some_prop"))
	})

	t.Run("values", func(t *testing.T) {
		c := &CallsChannel{
			ChannelID: "channelID",
			Props: StringMap{
				"bool_true":    true,
				"bool_false":   false,
				"string_true":  "true",
				"string_other": "yes",
				"number":       1,
			},
		}
		require.True(t, c.BoolProp("bool_true"))
		require.False(t, c.BoolProp("bool_false"))
		require.True(t, c.BoolProp("string_true"))
		require.False(t, c.BoolProp("string_other"))
		require.False(t, c.BoolProp("number"))
		require.False(t, c.BoolProp("missing"))
	})
}
