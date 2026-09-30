// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package public

import (
	"fmt"
)

type CallsChannel struct {
	ChannelID string    `json:"channel_id"`
	Enabled   bool      `json:"enabled"`
	Props     StringMap `json:"props,omitempty"`
}

func (c *CallsChannel) IsValid() error {
	if c == nil {
		return fmt.Errorf("should not be nil")
	}

	if c.ChannelID == "" {
		return fmt.Errorf("invalid ChannelID: should not be empty")
	}

	return nil
}

// BoolProp returns whether the given prop is set to true on the channel.
func (c *CallsChannel) BoolProp(key string) bool {
	if c == nil || c.Props == nil {
		return false
	}

	switch v := c.Props[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}
