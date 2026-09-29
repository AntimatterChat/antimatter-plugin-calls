// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package amenv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyName(t *testing.T) {
	require.Equal(t, "MM_CALLS_DISABLE", LegacyName("AM_CALLS_DISABLE"))
	require.Equal(t, "MM_CALLS_", LegacyName("AM_CALLS_"))
	require.Empty(t, LegacyName("CALLS_DISABLE"))
	require.Empty(t, LegacyName("MM_CALLS_DISABLE"))
}

func TestGet(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		require.Empty(t, Get("AM_CALLS_AMENV_TEST"))
	})

	t.Run("legacy fallback", func(t *testing.T) {
		t.Setenv("MM_CALLS_AMENV_TEST", "legacy")
		require.Equal(t, "legacy", Get("AM_CALLS_AMENV_TEST"))
	})

	t.Run("AM wins", func(t *testing.T) {
		t.Setenv("MM_CALLS_AMENV_TEST", "legacy")
		t.Setenv("AM_CALLS_AMENV_TEST", "new")
		require.Equal(t, "new", Get("AM_CALLS_AMENV_TEST"))
	})

	t.Run("empty AM falls back", func(t *testing.T) {
		t.Setenv("MM_CALLS_AMENV_TEST", "legacy")
		t.Setenv("AM_CALLS_AMENV_TEST", "")
		require.Equal(t, "legacy", Get("AM_CALLS_AMENV_TEST"))
	})
}

func TestLookup(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		_, ok := Lookup("AM_CALLS_AMENV_TEST")
		require.False(t, ok)
	})

	t.Run("legacy fallback", func(t *testing.T) {
		t.Setenv("MM_CALLS_AMENV_TEST", "legacy")
		v, ok := Lookup("AM_CALLS_AMENV_TEST")
		require.True(t, ok)
		require.Equal(t, "legacy", v)
	})

	t.Run("AM wins even when empty", func(t *testing.T) {
		t.Setenv("MM_CALLS_AMENV_TEST", "legacy")
		t.Setenv("AM_CALLS_AMENV_TEST", "")
		v, ok := Lookup("AM_CALLS_AMENV_TEST")
		require.True(t, ok)
		require.Empty(t, v)
	})
}

func TestWithPrefix(t *testing.T) {
	t.Setenv("MM_CALLS_AMENV_TEST_A", "legacy-a")
	t.Setenv("MM_CALLS_AMENV_TEST_B", "legacy-b")
	t.Setenv("AM_CALLS_AMENV_TEST_B", "new-b")
	t.Setenv("AM_CALLS_AMENV_TEST_C", "new-c")

	require.Equal(t, map[string]string{
		"A": "legacy-a",
		"B": "new-b",
		"C": "new-c",
	}, WithPrefix("AM_CALLS_AMENV_TEST_"))
}
