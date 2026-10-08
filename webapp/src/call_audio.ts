// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type CallsClient from './client';
import {logDebug, logErr} from './log';

type SinkAudioElement = HTMLAudioElement & {setSinkId?: (sinkId: string) => Promise<void>};

type PlayedTrack = {
    track: MediaStreamTrack;
    audioEl: SinkAudioElement;
    onEnded: () => void;

    // The session that sends it ('' when unknown), for its volume.
    sessionID: string;
};

// The volume of each participant's voice for this user (1 by default), by session, and the players to apply it to.
const sessionVolumes = new Map<string, number>();
const players = new Set<CallAudio>();

// setSessionVolume sets how loud a participant is heard, from 0 (silent) to 1 (as sent).
export function setSessionVolume(sessionID: string, volume: number) {
    const v = Math.min(1, Math.max(0, volume));
    if (v === 1) {
        sessionVolumes.delete(sessionID);
    } else {
        sessionVolumes.set(sessionID, v);
    }
    for (const player of players) {
        player.applyVolumes();
    }
}

export function getSessionVolume(sessionID: string): number {
    return sessionVolumes.get(sessionID) ?? 1;
}

// CallAudio plays the remote audio of a call (the participants' voices and the audio of a
// shared screen) through hidden <audio> elements, on the audio output device chosen for the
// call. It plays the client's tracks themselves, so disabling a track (e.g. to deafen) silences
// it. Whatever shows the call UI creates one per calls client.
export class CallAudio {
    private readonly client: CallsClient;
    private readonly played = new Map<string, PlayedTrack>();
    private sinkID: string;

    constructor(client: CallsClient) {
        this.client = client;
        this.sinkID = client.currentAudioOutputDevice?.deviceId || '';
        this.play(client.getRemoteVoiceTracks());
        client.on('remoteVoiceStream', this.onRemoteVoiceStream);
        client.on('devicechange', this.onDeviceChange);
        players.add(this);
    }

    public applyVolumes() {
        for (const played of this.played.values()) {
            played.audioEl.volume = getSessionVolume(played.sessionID);
        }
    }

    public destroy() {
        this.client.off('remoteVoiceStream', this.onRemoteVoiceStream);
        this.client.off('devicechange', this.onDeviceChange);
        players.delete(this);
        for (const trackID of [...this.played.keys()]) {
            this.stop(trackID);
        }
    }

    private onRemoteVoiceStream = (stream: MediaStream, sessionID?: string) => {
        this.play(stream.getAudioTracks(), sessionID);
    };

    // The client only records the chosen output device: apply it to the elements.
    private onDeviceChange = () => {
        const sinkID = this.client.currentAudioOutputDevice?.deviceId || '';
        if (sinkID === this.sinkID) {
            return;
        }
        logDebug('CallAudio: changing audio output device', sinkID);
        this.sinkID = sinkID;

        const ps = [];
        for (const {audioEl} of this.played.values()) {
            if (audioEl.setSinkId) {
                ps.push(audioEl.setSinkId(sinkID));
            }
        }
        Promise.all(ps).then(() => {
            logDebug('audio output has changed');
        }).catch((err) => {
            logErr(err);
        });
    };

    private play(tracks: MediaStreamTrack[], fromSession?: string) {
        for (const track of tracks) {
            if (this.played.has(track.id)) {
                continue;
            }

            const audioEl: SinkAudioElement = document.createElement('audio');
            audioEl.srcObject = new MediaStream([track]);
            audioEl.controls = false;
            audioEl.autoplay = true;
            audioEl.style.display = 'none';
            audioEl.onerror = (err) => logErr(err);
            audioEl.setAttribute('data-testid', track.id);
            const sessionID = fromSession ?? this.client.getSessionIDForTrack?.(track) ?? '';
            audioEl.volume = getSessionVolume(sessionID);

            if (this.sinkID && audioEl.setSinkId) {
                audioEl.setSinkId(this.sinkID).catch((err) => logErr(err));
            }

            document.body.appendChild(audioEl);

            const onEnded = () => this.stop(track.id);
            track.addEventListener('ended', onEnded);
            this.played.set(track.id, {track, audioEl, onEnded, sessionID});
        }
    }

    private stop(trackID: string) {
        const played = this.played.get(trackID);
        if (!played) {
            return;
        }
        this.played.delete(trackID);
        played.track.removeEventListener('ended', played.onEnded);
        played.audioEl.srcObject = null;
        played.audioEl.remove();
    }
}
