// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package rtc

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"
)

// newLoopbackPeerConnection is a peer connection that only uses the loopback interface.
func newLoopbackPeerConnection(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	m := &webrtc.MediaEngine{}
	require.NoError(t, m.RegisterDefaultCodecs())
	se := webrtc.SettingEngine{}
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetInterfaceFilter(func(name string) bool { return name == "lo" })
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pc.Close()
	})
	return pc
}

// TestSessionDeafenMedia checks over a real connection that a deafened session gets no packet of the audio rtcd
// forwards to it, and gets it again once undeafened.
func TestSessionDeafenMedia(t *testing.T) {
	// The session as rtcd holds it, sending someone's voice to the client, which receives it.
	s := &session{
		rtcConn:  newLoopbackPeerConnection(t),
		rxTracks: map[string]webrtc.TrackLocal{},
		silenced: map[*webrtc.RTPSender]webrtc.TrackLocal{},
	}
	client := newLoopbackPeerConnection(t)

	voice, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "voice_a", "stream_a")
	require.NoError(t, err)
	_, err = s.rtcConn.AddTrack(voice)
	require.NoError(t, err)

	var received atomic.Int64
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
			received.Add(1)
		}
	})

	offer, err := s.rtcConn.CreateOffer(nil)
	require.NoError(t, err)
	gathered := webrtc.GatheringCompletePromise(s.rtcConn)
	require.NoError(t, s.rtcConn.SetLocalDescription(offer))
	<-gathered
	require.NoError(t, client.SetRemoteDescription(*s.rtcConn.LocalDescription()))
	answer, err := client.CreateAnswer(nil)
	require.NoError(t, err)
	gathered = webrtc.GatheringCompletePromise(client)
	require.NoError(t, client.SetLocalDescription(answer))
	<-gathered
	require.NoError(t, s.rtcConn.SetRemoteDescription(*client.LocalDescription()))

	// rtcd forwarding the voice: a packet every 10ms.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var seq uint16
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				seq++
				_ = voice.WriteRTP(&rtp.Packet{
					Header:  rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 480},
					Payload: []byte{0xf8, 0xff, 0xfe},
				})
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	require.Eventually(t, func() bool { return received.Load() > 5 }, 10*time.Second, 10*time.Millisecond, "no audio before deafening")

	require.NoError(t, s.setDeafened(true))
	// Let what was already on its way arrive.
	time.Sleep(200 * time.Millisecond)
	silent := received.Load()
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, silent, received.Load(), "a deafened session received audio")

	require.NoError(t, s.setDeafened(false))
	require.Eventually(t, func() bool { return received.Load() > silent+5 }, 5*time.Second, 10*time.Millisecond, "no audio after undeafening")
}
