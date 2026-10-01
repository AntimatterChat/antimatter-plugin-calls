// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import CallsClient, {getSenderSessionIDFromSDP} from './client';

describe('CallsClient', () => {
    let client: CallsClient;
    let originalLocalStorage: Storage;

    beforeEach(() => {
        // Mock localStorage
        originalLocalStorage = window.localStorage;
        const localStorageMock = {
            getItem: jest.fn(),
            setItem: jest.fn(),
            clear: jest.fn(),
            removeItem: jest.fn(),
            key: jest.fn(),
            length: 0,
        };
        Object.defineProperty(window, 'localStorage', {
            value: localStorageMock,
            writable: true,
        });

        // Create a new client instance for each test
        client = new CallsClient({
            wsURL: 'wss://test.com',
            authToken: 'test-token',
            iceServers: [],
            enableAV1: false,
            enableVideo: false,
            dcSignaling: false,
            dcLocking: false,
        });

        // Mock the emit method to prevent errors
        client.emit = jest.fn();
    });

    afterEach(() => {
        // Restore original localStorage
        Object.defineProperty(window, 'localStorage', {
            value: originalLocalStorage,
            writable: true,
        });
    });

    describe('getSelectedAudioDevice', () => {
        it('should return null when no device is selected', () => {
            // Mock localStorage.getItem to return null
            jest.spyOn(window.localStorage, 'getItem').mockReturnValue(null);

            // @ts-ignore - accessing private method for testing
            const result = client.getSelectedAudioDevice('input');
            expect(result).toBeNull();
        });

        it('should return null when selected device is not found', () => {
            // Mock localStorage.getItem to return a device ID
            jest.spyOn(window.localStorage, 'getItem').mockReturnValue(JSON.stringify({
                deviceId: 'non-existent-device',
                label: 'Non-existent Device',
            }));

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [
                    {
                        deviceId: 'device1',
                        label: 'Device 1',
                        kind: 'audioinput',
                        groupId: '',
                        toJSON: jest.fn(),
                    } as MediaDeviceInfo,
                ],
                outputs: [],
            };

            // @ts-ignore - accessing private method for testing
            const result = client.getSelectedAudioDevice('input');
            expect(result).toBeNull();
        });

        it('should return the device when found by deviceId', () => {
            const expectedDevice = {
                deviceId: 'device1',
                label: 'Device 1',
                kind: 'audioinput',
                groupId: '',
                toJSON: jest.fn(),
            } as MediaDeviceInfo;

            // Mock localStorage.getItem to return a device ID
            jest.spyOn(window.localStorage, 'getItem').mockReturnValue(JSON.stringify({
                deviceId: 'device1',
                label: 'Device 1',
            }));

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [expectedDevice],
                outputs: [],
            };

            // @ts-ignore - accessing private method for testing
            const result = client.getSelectedAudioDevice('input');
            expect(result).toEqual(expectedDevice);
        });

        it('should return the device when found by label', () => {
            const expectedDevice = {deviceId: 'device1', label: 'Device 1', kind: 'audioinput' as MediaDeviceKind, groupId: '', toJSON: jest.fn()};

            // Mock localStorage.getItem to return a device with matching label but different ID
            jest.spyOn(window.localStorage, 'getItem').mockReturnValue(JSON.stringify({
                deviceId: 'old-device-id',
                label: 'Device 1',
            }));

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [expectedDevice],
                outputs: [],
            };

            // @ts-ignore - accessing private method for testing
            const result = client.getSelectedAudioDevice('input');
            expect(result).toEqual(expectedDevice);
        });

        it('should handle multiple devices with the same label', () => {
            const expectedDevice = {
                deviceId: 'device1',
                label: 'Same Label',
                kind: 'audioinput',
                groupId: '',
                toJSON: jest.fn(),
            } as MediaDeviceInfo;

            // Mock localStorage.getItem to return a device ID
            jest.spyOn(window.localStorage, 'getItem').mockReturnValue(JSON.stringify({
                deviceId: 'device1',
                label: 'Same Label',
            }));

            // Mock the audioDevices property with multiple devices having the same label
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [
                    expectedDevice,
                    {
                        deviceId: 'device2',
                        label: 'Same Label',
                        kind: 'audioinput',
                        groupId: '',
                        toJSON: jest.fn(),
                    } as MediaDeviceInfo,
                ],
                outputs: [],
            };

            // @ts-ignore - accessing private method for testing
            const result = client.getSelectedAudioDevice('input');
            expect(result).toEqual(expectedDevice);
        });

        it('should handle backward compatibility with string device IDs', () => {
            const expectedDevice = {deviceId: 'device1', label: 'Device 1', kind: 'audioinput' as MediaDeviceKind, groupId: '', toJSON: jest.fn()};

            // Mock localStorage.getItem to return just a string (old format)
            jest.spyOn(window.localStorage, 'getItem').mockReturnValue('device1');

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [expectedDevice],
                outputs: [],
            };

            // @ts-ignore - accessing private method for testing
            const result = client.getSelectedAudioDevice('input');
            expect(result).toEqual(expectedDevice);
        });
    });

    describe('handleAudioDeviceFallback', () => {
        it('should fall back to system default when current input device is missing', async () => {
            // Setup current device that's no longer available
            // @ts-ignore - accessing private property for testing
            client.currentAudioInputDevice = {deviceId: 'missing-device', label: 'Missing Device'};

            const defaultDevice = {
                deviceId: 'default-device',
                label: 'Default Device',
                kind: 'audioinput',
                groupId: '',
                toJSON: jest.fn(),
            } as MediaDeviceInfo;

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [defaultDevice],
                outputs: [],
            };

            // Mock setAudioInputDevice
            // @ts-ignore - accessing private method for testing
            client.setAudioInputDevice = jest.fn();

            // @ts-ignore - accessing private method for testing
            await client.handleAudioDeviceFallback('input');

            // @ts-ignore - accessing private method for testing
            expect(client.setAudioInputDevice).toHaveBeenCalledWith(defaultDevice, false);
        });

        it('should fall back to system default when current output device is missing', async () => {
            // Setup current device that's no longer available
            // @ts-ignore - accessing private property for testing
            client.currentAudioOutputDevice = {deviceId: 'missing-device', label: 'Missing Device'};

            const defaultDevice = {
                deviceId: 'default-device',
                label: 'Default Device',
                kind: 'audiooutput',
                groupId: '',
                toJSON: jest.fn(),
            } as MediaDeviceInfo;

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [],
                outputs: [defaultDevice],
            };

            // Mock setAudioOutputDevice
            // @ts-ignore - accessing private method for testing
            client.setAudioOutputDevice = jest.fn();

            // @ts-ignore - accessing private method for testing
            await client.handleAudioDeviceFallback('output');

            // @ts-ignore - accessing private method for testing
            expect(client.setAudioOutputDevice).toHaveBeenCalledWith(defaultDevice, false);
        });

        it('should switch to selected input device when it becomes available', async () => {
            // Setup current device
            // @ts-ignore - accessing private property for testing
            client.currentAudioInputDevice = {deviceId: 'current-device', label: 'Current Device'};

            const selectedDevice = {
                deviceId: 'selected-device',
                label: 'Selected Device',
                kind: 'audioinput',
                groupId: '',
                toJSON: jest.fn(),
            } as MediaDeviceInfo;

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [
                    {
                        deviceId: 'current-device',
                        label: 'Current Device',
                        kind: 'audioinput',
                        groupId: '',
                        toJSON: jest.fn(),
                    } as MediaDeviceInfo,
                    selectedDevice,
                ],
                outputs: [],
            };

            // Mock getSelectedAudioDevice to return the selected device
            // @ts-ignore - accessing private method for testing
            client.getSelectedAudioDevice = jest.fn().mockReturnValue(selectedDevice);

            // Mock setAudioInputDevice
            // @ts-ignore - accessing private method for testing
            client.setAudioInputDevice = jest.fn();

            // @ts-ignore - accessing private method for testing
            await client.handleAudioDeviceFallback('input');

            // @ts-ignore - accessing private method for testing
            expect(client.setAudioInputDevice).toHaveBeenCalledWith(selectedDevice, false);
        });

        it('should switch to selected output device when it becomes available', async () => {
            // Setup current device
            // @ts-ignore - accessing private property for testing
            client.currentAudioOutputDevice = {deviceId: 'current-device', label: 'Current Device'};

            const selectedDevice = {
                deviceId: 'selected-device',
                label: 'Selected Device',
                kind: 'audiooutput',
                groupId: '',
                toJSON: jest.fn(),
            } as MediaDeviceInfo;

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [],
                outputs: [
                    {
                        deviceId: 'current-device',
                        label: 'Current Device',
                        kind: 'audiooutput',
                        groupId: '',
                        toJSON: jest.fn(),
                    } as MediaDeviceInfo,
                    selectedDevice,
                ],
            };

            // Mock getSelectedAudioDevice to return the selected device
            // @ts-ignore - accessing private method for testing
            client.getSelectedAudioDevice = jest.fn().mockReturnValue(selectedDevice);

            // Mock setAudioOutputDevice
            // @ts-ignore - accessing private method for testing
            client.setAudioOutputDevice = jest.fn();

            // @ts-ignore - accessing private method for testing
            await client.handleAudioDeviceFallback('output');

            // @ts-ignore - accessing private method for testing
            expect(client.setAudioOutputDevice).toHaveBeenCalledWith(selectedDevice, false);
        });

        it('should do nothing when current device is available and matches selected device', async () => {
            // Setup current device
            const currentDevice = {deviceId: 'current-device', label: 'Current Device'};

            // @ts-ignore - accessing private property for testing
            client.currentAudioInputDevice = currentDevice;

            // Mock the audioDevices property
            // @ts-ignore - accessing private property for testing
            client.audioDevices = {
                inputs: [
                    {
                        deviceId: 'current-device',
                        label: 'Current Device',
                        kind: 'audioinput',
                        groupId: '',
                        toJSON: jest.fn(),
                    } as MediaDeviceInfo,
                ],
                outputs: [],
            };

            // Mock getSelectedAudioDevice to return the current device
            // @ts-ignore - accessing private method for testing
            client.getSelectedAudioDevice = jest.fn().mockReturnValue(currentDevice);

            // Mock setAudioInputDevice
            // @ts-ignore - accessing private method for testing
            client.setAudioInputDevice = jest.fn();

            // @ts-ignore - accessing private method for testing
            await client.handleAudioDeviceFallback('input');

            // @ts-ignore - accessing private method for testing
            expect(client.setAudioInputDevice).not.toHaveBeenCalled();
        });
    });

    describe('video track management', () => {
        const originalMediaDevices = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices');

        afterEach(() => {
            // Restore navigator.mediaDevices in case a test replaced it, to avoid polluting other tests.
            if (originalMediaDevices) {
                Object.defineProperty(navigator, 'mediaDevices', originalMediaDevices);
            } else {
                // @ts-ignore - cleaning up a mock added by a test
                delete navigator.mediaDevices;
            }
        });

        const makeVideoTrack = (id: string) => ({
            id,
            kind: 'video',
            enabled: false,
            stop: jest.fn(),
            dispatchEvent: jest.fn(),
        });

        const makeStream = (id: string, tracks: Array<ReturnType<typeof makeVideoTrack>>) => ({
            id,
            getVideoTracks: () => tracks,
            getTracks: () => tracks,
            removeTrack: jest.fn(),
            addTrack: jest.fn(),
        });

        const setupPeer = () => {
            const peer = {addTrack: jest.fn(), replaceTrack: jest.fn()};
            const ws = {send: jest.fn()};

            // @ts-ignore - accessing private property for testing
            client.config.enableVideo = true;

            // @ts-ignore - accessing private property for testing
            client.peer = peer;

            // @ts-ignore - accessing private property for testing
            client.ws = ws;
            return {peer, ws};
        };

        it('startVideo adds the track and records the sender track ID', async () => {
            const {peer} = setupPeer();
            const track = makeVideoTrack('cam-1');
            const stream = makeStream('stream-1', [track]);

            // @ts-ignore - accessing private property for testing
            client.localVideoStream = stream;

            await client.startVideo();

            expect(peer.addTrack).toHaveBeenCalledTimes(1);
            expect(track.enabled).toBe(true);

            // @ts-ignore - accessing private property for testing
            expect(client.videoTrackAdded).toBe(true);

            // @ts-ignore - accessing private property for testing
            expect(client.videoSenderTrackID).toBe('cam-1');
        });

        it('stopVideo stops the camera track and releases the stream so the device LED turns off', () => {
            const {peer, ws} = setupPeer();
            const track = makeVideoTrack('cam-1');
            track.enabled = true;
            const stream = makeStream('stream-1', [track]);

            // @ts-ignore - accessing private property for testing
            client.localVideoStream = stream;

            // @ts-ignore - accessing private property for testing
            client.videoSenderTrackID = 'cam-1';

            client.stopVideo();

            // Detaches the track from the sender (which is kept alive) before stopping it.
            expect(peer.replaceTrack).toHaveBeenCalledWith('cam-1', null);

            // Crucially, the device is fully stopped (not just disabled) so the LED turns off,
            // and an 'ended' event is dispatched so listeners can react.
            expect(track.stop).toHaveBeenCalledTimes(1);
            expect(track.dispatchEvent).toHaveBeenCalledWith(expect.objectContaining({type: 'ended'}));

            // The stream is released so the next startVideo re-initializes it.
            // @ts-ignore - accessing private property for testing
            expect(client.localVideoStream).toBeNull();
            expect(ws.send).toHaveBeenCalledWith('video_off');
        });

        it('stopVideo stops and clears the background-blur segmenter when active', () => {
            setupPeer();
            const track = makeVideoTrack('cam-1');
            track.enabled = true;
            const stream = makeStream('stream-1', [track]);
            const segmenter = {stop: jest.fn()};

            // @ts-ignore - accessing private property for testing
            client.localVideoStream = stream;

            // @ts-ignore - accessing private property for testing
            client.videoSenderTrackID = 'cam-1';

            // @ts-ignore - accessing private property for testing
            client.segmenter = segmenter;

            client.stopVideo();

            expect(segmenter.stop).toHaveBeenCalledTimes(1);

            // @ts-ignore - accessing private property for testing
            expect(client.segmenter).toBeNull();
        });

        it('re-enables video after a stop by replacing the sender track with a freshly acquired one', async () => {
            const {peer} = setupPeer();

            const track1 = makeVideoTrack('cam-1');
            const stream1 = makeStream('stream-1', [track1]);

            // @ts-ignore - accessing private property for testing
            client.localVideoStream = stream1;

            await client.startVideo();
            client.stopVideo();

            // initVideo acquires a brand new stream/track on re-enable.
            const track2 = makeVideoTrack('cam-2');
            const stream2 = makeStream('stream-2', [track2]);

            // @ts-ignore - accessing private method for testing
            client.initVideo = jest.fn().mockImplementation(async () => {
                // @ts-ignore - accessing private property for testing
                client.localVideoStream = stream2;
            });

            await client.startVideo();

            // The sender is reused (no second addTrack) and the new track replaces the
            // previously held one, keyed by the tracked ID.
            expect(peer.addTrack).toHaveBeenCalledTimes(1);
            expect(peer.replaceTrack).toHaveBeenLastCalledWith('cam-1', track2);

            // @ts-ignore - accessing private property for testing
            expect(client.videoSenderTrackID).toBe('cam-2');
        });

        it('keeps the sender track ID in sync after a device switch so a later stopVideo targets the right sender (MM-68796 regression)', async () => {
            const {peer} = setupPeer();

            const oldTrack = makeVideoTrack('cam-1');
            const oldStream = makeStream('stream-1', [oldTrack]);

            // @ts-ignore - accessing private property for testing
            client.localVideoStream = oldStream;

            await client.startVideo();

            // @ts-ignore - accessing private property for testing
            expect(client.videoSenderTrackID).toBe('cam-1');

            // Switching the camera replaces the sender track, which re-keys the peer's
            // senders map. Before the fix, videoSenderTrackID kept pointing at the old ID,
            // and the subsequent stopVideo threw "senders for track not found".
            const newTrack = makeVideoTrack('cam-2');
            const newStream = makeStream('stream-2', [newTrack]);
            Object.defineProperty(navigator, 'mediaDevices', {
                value: {getUserMedia: jest.fn().mockResolvedValue(newStream)},
                writable: true,
                configurable: true,
            });

            await client.setVideoInputDevice({deviceId: 'cam-2-device', label: 'Camera 2'} as MediaDeviceInfo);

            expect(oldTrack.stop).toHaveBeenCalledTimes(1);
            expect(peer.replaceTrack).toHaveBeenLastCalledWith('cam-1', newTrack);

            // @ts-ignore - accessing private property for testing
            expect(client.videoSenderTrackID).toBe('cam-2');

            client.stopVideo();

            // The detach now targets the current sender track, not the stale one.
            expect(peer.replaceTrack).toHaveBeenLastCalledWith('cam-2', null);
            expect(newTrack.stop).toHaveBeenCalledTimes(1);
        });
    });
    describe('remote streams', () => {
        const sessionA = 'aaaaaaaaaaaaaaaaaaaaaaaaaa';
        const sessionB = 'bbbbbbbbbbbbbbbbbbbbbbbbbb';
        const originalMediaStream = global.MediaStream;

        beforeEach(() => {
            // @ts-ignore - jsdom has no MediaStream
            global.MediaStream = jest.fn((tracks: MediaStreamTrack[]) => ({
                getTracks: () => tracks,
                getAudioTracks: () => tracks.filter((t) => t.kind === 'audio'),
                getVideoTracks: () => tracks.filter((t) => t.kind === 'video'),
            }));
        });

        afterEach(() => {
            global.MediaStream = originalMediaStream;
        });

        const makeTrack = (kind: string, readyState = 'live') => ({kind, id: `${kind}-track`, label: '', readyState} as unknown as MediaStreamTrack);

        const receive = (track: MediaStreamTrack, type: string) => {
            const stream = new MediaStream([track]);

            // @ts-ignore - calling a private method for testing
            client.handleRemoteStream(stream, {type, sender_id: ''});
            return stream;
        };

        const setupPeer = (receivers: Array<{mid: string, track: MediaStreamTrack}>) => {
            const sdp = receivers.map(({mid, track}, i) => [
                `m=${track.kind} 9 UDP/TLS/RTP/SAVPF 96`,
                `a=mid:${mid}`,
                `a=msid:${i % 2 ? sessionB : sessionA} ${track.kind === 'audio' ? 'voice' : 'video'}_${i % 2 ? sessionB : sessionA}_abcd1234`,
            ].join('\r\n')).join('\r\n');

            // @ts-ignore - accessing private property for testing
            client.peer = {
                pc: {
                    remoteDescription: {sdp: 'v=0\r\n' + sdp},
                    getTransceivers: () => receivers.map(({mid, track}) => ({mid, receiver: {track}})),
                },
            };
        };

        it('emits the sender session with remote streams', () => {
            const voice = makeTrack('audio');
            const video = makeTrack('video');
            setupPeer([{mid: '0', track: voice}, {mid: '1', track: video}]);

            const voiceStream = receive(voice, 'voice');
            const videoStream = receive(video, 'video');

            expect(client.emit).toHaveBeenCalledWith('remoteVoiceStream', voiceStream, sessionA);
            expect(client.emit).toHaveBeenCalledWith('remoteVideoStream', videoStream, sessionB);
        });

        it('keeps the live video stream of every sender', () => {
            const videoA = makeTrack('video');
            const videoB = makeTrack('video');
            setupPeer([{mid: '0', track: videoA}, {mid: '1', track: videoB}]);

            receive(videoA, 'video');
            receive(videoB, 'video');

            let streams = client.getRemoteVideoStreams();
            expect(Object.keys(streams).sort()).toEqual([sessionA, sessionB]);
            expect(streams[sessionA].getTracks()).toEqual([videoA]);
            expect(streams[sessionB].getTracks()).toEqual([videoB]);

            // @ts-ignore - simulating an ended track
            videoA.readyState = 'ended';
            streams = client.getRemoteVideoStreams();
            expect(Object.keys(streams)).toEqual([sessionB]);
        });

        it('handles an unknown sender', () => {
            const video = makeTrack('video');

            // @ts-ignore - accessing private property for testing
            client.peer = null;
            const stream = receive(video, 'video');

            expect(client.emit).toHaveBeenCalledWith('remoteVideoStream', stream, '');
            expect(client.getRemoteVideoStreams()).toEqual({});
            expect(client.getRemoteVideoStream()?.getTracks()).toEqual([video]);
        });
    });
});

describe('getSenderSessionIDFromSDP', () => {
    const sessionA = 'aaaaaaaaaaaaaaaaaaaaaaaaaa';
    const sessionB = 'bbbbbbbbbbbbbbbbbbbbbbbbbb';
    const sdp = [
        'v=0',
        'o=- 123 2 IN IP4 0.0.0.0',
        's=-',
        't=0 0',
        'm=audio 9 UDP/TLS/RTP/SAVPF 111',
        'a=mid:0',
        `a=msid:${sessionA} voice_${sessionA}_abcd1234`,
        'm=video 9 UDP/TLS/RTP/SAVPF 96',
        'a=mid:1',
        `a=msid:${sessionB} video_${sessionB}_efgh5678`,
        'm=video 9 UDP/TLS/RTP/SAVPF 96',
        'a=mid:2',
        'a=msid:streamid othertrack',
        'm=video 9 UDP/TLS/RTP/SAVPF 96',
        'a=mid:3',
        'a=msid:- othertrack',
        'm=application 9 UDP/DTLS/SCTP webrtc-datachannel',
        'a=mid:4',
        '',
    ].join('\r\n');

    it('reads the sender from the forwarded track ID', () => {
        expect(getSenderSessionIDFromSDP(sdp, '0')).toBe(sessionA);
        expect(getSenderSessionIDFromSDP(sdp, '1')).toBe(sessionB);
    });

    it('falls back to the stream ID', () => {
        expect(getSenderSessionIDFromSDP(sdp, '2')).toBe('streamid');
    });

    it('returns an empty string when unknown', () => {
        expect(getSenderSessionIDFromSDP(sdp, '3')).toBe('');
        expect(getSenderSessionIDFromSDP(sdp, '4')).toBe('');
        expect(getSenderSessionIDFromSDP(sdp, '5')).toBe('');
        expect(getSenderSessionIDFromSDP('', '0')).toBe('');
    });
});
