// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package public

import (
	"fmt"
)

// Channel props understood by Calls. They let an administrator or another plugin adapt how
// calls behave in a specific channel; a channel without them behaves as usual. Values are
// booleans (the string "true" is accepted too).
const (
	// ChannelPropBroadcastSessionState makes the events about the state of call sessions
	// (muted/unmuted, voice on/off, screen on/off, video on/off, raised/lowered hand) go to
	// every member of the channel instead of only to the call participants, so that clients
	// can show who is talking or sharing to people outside the call.
	ChannelPropBroadcastSessionState = "broadcast_session_state"
	// ChannelPropDisableCallPost stops Calls from posting a "call started" message when a call
	// starts in the channel, e.g. for channels where people drop in and out all day long.
	// Recordings and transcriptions need that post and are unavailable in such channels.
	ChannelPropDisableCallPost = "disable_call_post"
	// ChannelPropEnableVideo lets participants turn on their camera in calls in the channel,
	// when video is enabled in the plugin configuration. Without it, video is only offered in
	// direct messages. The call widget keeps its audio-only layout in such channels.
	ChannelPropEnableVideo = "enable_video"
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
