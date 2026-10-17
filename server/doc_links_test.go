// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The links the app shows (the System Console's header, the server's messages, the web app) go to
// Antimatter's docs, not to the dead docs.antimatter.example of the rebrand.
func TestNoDeadDocsLinks(t *testing.T) {
	var files []string
	for _, root := range []string{"../plugin.json", "i18n", "../webapp/src", "../webapp/i18n"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".json", ".ts", ".tsx", ".js", ".jsx":
				files = append(files, path)
			}
			return nil
		})
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
	}
	require.Contains(t, files, "../plugin.json")

	for _, path := range files {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.False(t, strings.Contains(string(data), "docs.antimatter.example"), "%s links to docs.antimatter.example", path)
	}

	manifest, err := os.ReadFile("../plugin.json")
	require.NoError(t, err)
	require.Contains(t, string(manifest), "https://antimatter.chat/docs/admins/calls.html", "the System Console's header links to the Calls docs")
}
