// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// Package amenv reads the plugin's own environment variables. The AM_-prefixed
// name is authoritative; the legacy MM_-prefixed name is still accepted as a
// fallback so existing deployments keep working. When both are set, AM_ wins.
package amenv

import (
	"os"
	"strings"
)

const (
	// Prefix is the prefix of the plugin's environment variables.
	Prefix = "AM_"
	// LegacyPrefix is the prefix still accepted as a fallback.
	LegacyPrefix = "MM_"
)

// LegacyName returns the MM_-prefixed fallback for an AM_-prefixed name, or ""
// when name does not start with AM_.
func LegacyName(name string) string {
	if rest, ok := strings.CutPrefix(name, Prefix); ok {
		return LegacyPrefix + rest
	}
	return ""
}

// Lookup returns the value of the AM_-prefixed variable name when it is set
// (even to an empty value), otherwise the value of its MM_-prefixed legacy
// counterpart. The boolean reports whether either variable is set.
func Lookup(name string) (string, bool) {
	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	if legacy := LegacyName(name); legacy != "" {
		return os.LookupEnv(legacy)
	}
	return "", false
}

// Get returns the value of the AM_-prefixed variable name when it is set to a
// non-empty value, otherwise the value of its MM_-prefixed legacy counterpart.
func Get(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if legacy := LegacyName(name); legacy != "" {
		return os.Getenv(legacy)
	}
	return ""
}

// WithPrefix returns every environment variable whose name starts with the
// AM_-prefixed prefix, or with its MM_-prefixed legacy counterpart, keyed by the
// remainder of the name after the prefix. When the same key is set under both
// prefixes the AM_ value wins.
func WithPrefix(prefix string) map[string]string {
	prefixes := []string{prefix}
	if legacy := LegacyName(prefix); legacy != "" {
		// Legacy first so that the AM_ value overwrites it.
		prefixes = []string{legacy, prefix}
	}

	environ := os.Environ()
	vars := make(map[string]string)
	for _, p := range prefixes {
		for _, env := range environ {
			name, value, ok := strings.Cut(env, "=")
			if !ok {
				continue
			}
			if key, found := strings.CutPrefix(name, p); found {
				vars[key] = value
			}
		}
	}
	return vars
}
