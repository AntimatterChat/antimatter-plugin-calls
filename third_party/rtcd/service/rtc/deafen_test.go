// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package rtc

import (
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"
)

func newDeafenTestSession(t *testing.T) *session {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pc.Close())
	})
	return &session{
		rtcConn:  pc,
		rxTracks: map[string]webrtc.TrackLocal{},
		silenced: map[*webrtc.RTPSender]webrtc.TrackLocal{},
	}
}

func addDeafenTestTrack(t *testing.T, s *session, mimeType, id string) (*webrtc.TrackLocalStaticRTP, *webrtc.RTPSender) {
	t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: mimeType}, id, "stream_"+id)
	require.NoError(t, err)
	sender, err := s.rtcConn.AddTrack(track)
	require.NoError(t, err)
	return track, sender
}

func TestSessionDeafen(t *testing.T) {
	t.Run("audio is silenced, video goes through, and undeafening restores it", func(t *testing.T) {
		s := newDeafenTestSession(t)
		voice, voiceSender := addDeafenTestTrack(t, s, webrtc.MimeTypeOpus, "voice_a")
		screenAudio, screenAudioSender := addDeafenTestTrack(t, s, webrtc.MimeTypeOpus, "screen-audio_a")
		video, videoSender := addDeafenTestTrack(t, s, webrtc.MimeTypeVP8, "video_a")

		require.NoError(t, s.setDeafened(true))
		require.True(t, s.deafened)
		for sender, track := range map[*webrtc.RTPSender]*webrtc.TrackLocalStaticRTP{voiceSender: voice, screenAudioSender: screenAudio} {
			twin := sender.Track()
			require.NotNil(t, twin)
			require.NotSame(t, track, twin)

			// The twin keeps the SDP as it was.
			require.Equal(t, track.ID(), twin.ID())
			require.Equal(t, track.StreamID(), twin.StreamID())
			require.Equal(t, track.Kind(), twin.Kind())
			require.Same(t, track, s.senderTrack(sender))
		}
		require.Same(t, video, videoSender.Track())
		require.Len(t, s.silenced, 2)

		// Deafening again changes nothing.
		twin := voiceSender.Track()
		require.NoError(t, s.setDeafened(true))
		require.Same(t, twin, voiceSender.Track())

		require.NoError(t, s.setDeafened(false))
		require.False(t, s.deafened)
		require.Same(t, voice, voiceSender.Track())
		require.Same(t, screenAudio, screenAudioSender.Track())
		require.Same(t, video, videoSender.Track())
		require.Empty(t, s.silenced)
	})

	t.Run("tracks added while deafened are silenced", func(t *testing.T) {
		s := newDeafenTestSession(t)
		require.NoError(t, s.setDeafened(true))

		voice, voiceSender := addDeafenTestTrack(t, s, webrtc.MimeTypeOpus, "voice_b")
		s.mut.Lock()
		require.NoError(t, s.silenceSender(voiceSender, voice))
		s.mut.Unlock()
		require.NotSame(t, voice, voiceSender.Track())
		require.Same(t, voice, s.senderTrack(voiceSender))

		require.NoError(t, s.setDeafened(false))
		require.Same(t, voice, voiceSender.Track())
	})

	t.Run("a track cleaned up while deafened isn't restored", func(t *testing.T) {
		s := newDeafenTestSession(t)
		_, voiceSender := addDeafenTestTrack(t, s, webrtc.MimeTypeOpus, "voice_c")
		require.NoError(t, s.setDeafened(true))

		// As handleSessionClose does once the sender left.
		require.NoError(t, voiceSender.ReplaceTrack(nil))

		require.NoError(t, s.setDeafened(false))
		require.Nil(t, voiceSender.Track())
		require.Empty(t, s.silenced)
	})

	t.Run("nothing to silence", func(t *testing.T) {
		s := newDeafenTestSession(t)
		_, videoSender := addDeafenTestTrack(t, s, webrtc.MimeTypeVP8, "video_d")
		s.mut.Lock()
		require.NoError(t, s.silenceSender(nil, nil))
		require.NoError(t, s.silenceSender(videoSender, videoSender.Track()))
		s.mut.Unlock()
		require.Empty(t, s.silenced)
	})

	t.Run("session props", func(t *testing.T) {
		require.True(t, SessionProps{"deafened": true}.Deafened())
		require.False(t, SessionProps{}.Deafened())

		var cfg SessionConfig
		require.NoError(t, cfg.FromMap(map[string]any{"callID": "c", "deafened": true}))
		require.True(t, cfg.Props.Deafened())
	})
}
