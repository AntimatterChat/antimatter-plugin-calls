// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {EventEmitter} from 'events';

import {CallAudio} from './call_audio';
import type CallsClient from './client';

class FakeTrack extends EventTarget {
    id: string;
    kind = 'audio';
    enabled = true;

    constructor(id: string) {
        super();
        this.id = id;
    }
}

class FakeClient extends EventEmitter {
    currentAudioOutputDevice: {deviceId: string} | null = null;
    tracks: FakeTrack[] = [];

    getRemoteVoiceTracks() {
        return this.tracks;
    }
}

const audioEls = () => [...document.querySelectorAll('audio')] as Array<HTMLAudioElement & {setSinkId: jest.Mock}>;

describe('CallAudio', () => {
    const originalMediaStream = global.MediaStream;
    const originalSetSinkId = (HTMLMediaElement.prototype as unknown as {setSinkId?: unknown}).setSinkId;
    let setSinkId: jest.Mock;

    beforeEach(() => {
        // @ts-ignore - jsdom has no MediaStream
        global.MediaStream = jest.fn((tracks: MediaStreamTrack[]) => ({getTracks: () => tracks}));
        setSinkId = jest.fn(() => Promise.resolve());
        (HTMLMediaElement.prototype as unknown as {setSinkId?: unknown}).setSinkId = setSinkId;
    });

    afterEach(() => {
        global.MediaStream = originalMediaStream;
        (HTMLMediaElement.prototype as unknown as {setSinkId?: unknown}).setSinkId = originalSetSinkId;
        document.body.innerHTML = '';
    });

    const setup = () => {
        const client = new FakeClient();
        const audio = new CallAudio(client as unknown as CallsClient);
        return {client, audio};
    };

    test('plays the tracks already received and those received later, once each', () => {
        const client = new FakeClient();
        client.tracks = [new FakeTrack('t1')];
        const audio = new CallAudio(client as unknown as CallsClient);

        const t2 = new FakeTrack('t2');
        client.emit('remoteVoiceStream', {getAudioTracks: () => [t2]}, 'session');
        client.emit('remoteVoiceStream', {getAudioTracks: () => [t2]}, 'session');

        expect(audioEls().map((el) => el.getAttribute('data-testid'))).toEqual(['t1', 't2']);
        audio.destroy();
    });

    test('removes the element of a track that ended', () => {
        const {client, audio} = setup();
        const track = new FakeTrack('t1');
        client.emit('remoteVoiceStream', {getAudioTracks: () => [track]});
        expect(audioEls()).toHaveLength(1);

        track.dispatchEvent(new Event('ended'));
        expect(audioEls()).toHaveLength(0);
        audio.destroy();
    });

    test('moves the audio to the chosen output device', () => {
        const {client, audio} = setup();
        client.emit('remoteVoiceStream', {getAudioTracks: () => [new FakeTrack('t1'), new FakeTrack('t2')]});
        expect(setSinkId).not.toHaveBeenCalled();

        client.currentAudioOutputDevice = {deviceId: 'speakers'};
        client.emit('devicechange');
        expect(setSinkId).toHaveBeenCalledTimes(2);
        expect(setSinkId).toHaveBeenCalledWith('speakers');

        // Unrelated device changes don't touch the elements
        client.emit('devicechange');
        expect(setSinkId).toHaveBeenCalledTimes(2);

        // New tracks play on the chosen device
        client.emit('remoteVoiceStream', {getAudioTracks: () => [new FakeTrack('t3')]});
        expect(setSinkId).toHaveBeenCalledTimes(3);
        audio.destroy();
    });

    test('stops playing and listening when destroyed', () => {
        const {client, audio} = setup();
        client.emit('remoteVoiceStream', {getAudioTracks: () => [new FakeTrack('t1')]});

        audio.destroy();
        expect(audioEls()).toHaveLength(0);
        expect(client.listenerCount('remoteVoiceStream')).toBe(0);
        expect(client.listenerCount('devicechange')).toBe(0);
    });
});
