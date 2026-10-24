// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package rtc

import (
	"errors"
	"fmt"

	"github.com/pion/webrtc/v4"
)

// Antimatter: deafening a session, for the moderators' server deafen. A deafened session receives no audio: the
// sender of every audio track it receives (other people's voices and their screens' audio) sends a silent twin of
// the track instead, a track with the same ID and stream ID that nothing writes to. The twin keeps the SDP (msid,
// SSRC) as it was, so neither deafening nor a renegotiation while deafened changes anything for the client but the
// silence, and undeafening puts the real track back without renegotiating. Video and screens still go through.
//
// The Calls plugin also keeps a deafened session muted (MuteMessage), so that it can't be heard either.

// silentTwin gives a track like the given one, which nothing writes to.
func silentTwin(track webrtc.TrackLocal) (webrtc.TrackLocal, error) {
	static, ok := track.(*webrtc.TrackLocalStaticRTP)
	if !ok {
		return nil, fmt.Errorf("unexpected track type %T", track)
	}
	return webrtc.NewTrackLocalStaticRTP(static.Codec(), static.ID(), static.StreamID())
}

// silenceSender makes an audio sender send the silent twin of its track. Needs s.mut.
func (s *session) silenceSender(sender *webrtc.RTPSender, track webrtc.TrackLocal) error {
	if sender == nil || track == nil || track.Kind() != webrtc.RTPCodecTypeAudio {
		return nil
	}
	if _, ok := s.silenced[sender]; ok {
		return nil
	}
	twin, err := silentTwin(track)
	if err != nil {
		return fmt.Errorf("failed to create silent track for %s: %w", track.ID(), err)
	}
	if err := sender.ReplaceTrack(twin); err != nil {
		return fmt.Errorf("failed to silence track %s: %w", track.ID(), err)
	}
	s.silenced[sender] = track
	return nil
}

// senderTrack is the track a sender is for: its real track when it sends the silent twin. Needs s.mut.
func (s *session) senderTrack(sender *webrtc.RTPSender) webrtc.TrackLocal {
	if track, ok := s.silenced[sender]; ok {
		return track
	}
	return sender.Track()
}

// setDeafened deafens the session, or gives it its audio back.
func (s *session) setDeafened(deafened bool) error {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.deafened = deafened
	var errs []error
	if deafened {
		for _, sender := range s.rtcConn.GetSenders() {
			if err := s.silenceSender(sender, sender.Track()); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	for sender, track := range s.silenced {
		delete(s.silenced, sender)

		// A sender whose track was cleaned up (its sender left) has nothing to get back.
		if sender.Track() == nil {
			continue
		}
		if err := sender.ReplaceTrack(track); err != nil {
			errs = append(errs, fmt.Errorf("failed to restore track %s: %w", track.ID(), err))
		}
	}
	return errors.Join(errs...)
}
