// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {GlobalState} from '@mattermost/types/store';
import {EventEmitter} from 'events';
import {hostMute} from 'src/actions';
import CallsClient, {AudioInputPermissionsError} from 'src/client';
import {ChannelType} from 'src/types/types';

import {createCallsAPI, selectors} from './public_api';
import {Store} from './types/antimatter-webapp';

jest.mock('src/actions', () => ({
    dismissIncomingCallNotification: jest.fn(() => ({type: 'dismiss'})),
    hostLowerHand: jest.fn(() => Promise.resolve()),
    hostMute: jest.fn(() => Promise.resolve()),
    hostRemove: jest.fn(() => Promise.resolve()),
    hostScreenOff: jest.fn(() => Promise.resolve()),
    openCallsUserSettings: jest.fn(() => ({type: 'settings'})),
    showScreenSourceModal: jest.fn(() => ({type: 'screen-source'})),
}));

const channelID = 'channel-id';
const currentUserID = 'me';

const session = (id: string, userID: string, extra = {}) => ({session_id: id, user_id: userID, unmuted: false, raised_hand: 0, ...extra});

type StateOpts = {
    sessions?: Record<string, ReturnType<typeof session>>;
    localCall?: {channelID: string; sessionID: string; state: string} | null;
    clientStateReducer?: {channelID: string; sessionID: string} | null;
    screenSharingIDs?: Record<string, string>;
    hosts?: Record<string, {hostID: string}>;
    incomingCalls?: unknown[];
    callsConfig?: Record<string, unknown>;
};

const makeState = (opts: StateOpts = {}) => ({
    'plugins-com.mattermost.calls': {
        channels: {},
        calls: {[channelID]: {ID: 'call-id', channelID, startAt: 10, ownerID: 'owner', threadID: 'thread'}},
        sessions: {[channelID]: opts.sessions || {}},
        screenSharingIDs: opts.screenSharingIDs || {},
        hosts: opts.hosts || {[channelID]: {hostID: 'owner'}},
        recordings: {},
        localCall: opts.localCall ?? null,
        clientStateReducer: opts.clientStateReducer ?? null,
        incomingCalls: opts.incomingCalls || [],
        callsConfig: {AllowScreenSharing: true, DefaultEnabled: true, ...opts.callsConfig},
    },
    entities: {
        users: {currentUserId: currentUserID, profiles: {}},
        channels: {channels: {}},
        general: {config: {}},
    },
} as unknown as GlobalState);

describe('selectors', () => {
    test('getParticipants maps the sessions and keeps the result while unchanged', () => {
        const sessions = {
            s1: session('s1', 'owner', {unmuted: true, voice: true}),
            s2: session('s2', currentUserID, {video: true, raised_hand: 5}),
        };
        const state = makeState({sessions, screenSharingIDs: {[channelID]: 's1'}, localCall: {channelID, sessionID: 's2', state: 'connected'}});

        const participants = selectors.getParticipants(state, channelID);
        expect(participants).toEqual([
            {sessionId: 's1', userId: 'owner', muted: false, video: false, screenSharing: true, speaking: true, raisedHand: false, isHost: true, isMe: false},
            {sessionId: 's2', userId: currentUserID, muted: true, video: true, screenSharing: false, speaking: false, raisedHand: true, isHost: false, isMe: true},
        ]);

        // Another state with the same call slices gives the same array
        const sameCall = makeState({sessions, screenSharingIDs: {[channelID]: 's1'}, localCall: {channelID, sessionID: 's2', state: 'connected'}});
        expect(selectors.getParticipants(sameCall, channelID)).toBe(participants);

        // A change gives a new one
        const changed = makeState({sessions: {...sessions}, screenSharingIDs: {[channelID]: 's1'}, localCall: {channelID, sessionID: 's2', state: 'connected'}});
        expect(selectors.getParticipants(changed, channelID)).not.toBe(participants);
    });

    test('getParticipants returns a shared empty array for channels without a call', () => {
        const state = makeState();
        expect(selectors.getParticipants(state, 'other')).toEqual([]);
        expect(selectors.getParticipants(state, 'other')).toBe(selectors.getParticipants(makeState(), 'another'));
    });

    test('getLocalCall tells where the media is', () => {
        expect(selectors.getLocalCall(makeState())).toBeNull();

        const local = {channelID, sessionID: '', state: 'connecting'};
        const inWindow = selectors.getLocalCall(makeState({localCall: local}));
        expect(inWindow).toEqual({channelId: channelID, sessionId: '', state: 'connecting', mediaInThisWindow: true});
        expect(selectors.getLocalCall(makeState({localCall: local}))).toBe(inWindow);

        expect(selectors.getLocalCall(makeState({clientStateReducer: {channelID, sessionID: 'desktop'}}))).toEqual(
            {channelId: channelID, sessionId: 'desktop', state: 'connected', mediaInThisWindow: false},
        );
    });

    test('getCall', () => {
        const call = selectors.getCall(makeState(), channelID);
        expect(call).toEqual({channelId: channelID, id: 'call-id', startAt: 10, ownerId: 'owner', hostId: 'owner', threadId: 'thread', recording: false});
        expect(selectors.getCall(makeState(), 'other')).toBeNull();
    });

    test('getIncomingCalls sorts the newest first and keeps the result while unchanged', () => {
        const incomingCalls = [
            {callID: 'c1', channelID: 'ch1', callerID: 'u1', startAt: 1, type: ChannelType.DM},
            {callID: 'c2', channelID: 'ch2', callerID: 'u2', startAt: 2, type: ChannelType.GM},
        ];
        const state = makeState({incomingCalls});
        const calls = selectors.getIncomingCalls(state);
        expect(calls).toEqual([
            {channelId: 'ch2', callId: 'c2', callerId: 'u2', startAt: 2, type: 'gm'},
            {channelId: 'ch1', callId: 'c1', callerId: 'u1', startAt: 1, type: 'dm'},
        ]);
        expect(selectors.getIncomingCalls(makeState({incomingCalls}))).toBe(calls);
    });

    test('getMyMuted', () => {
        expect(selectors.getMyMuted(makeState())).toBe(true);
        const sessions = {s1: session('s1', currentUserID, {unmuted: true})};
        expect(selectors.getMyMuted(makeState({sessions, localCall: {channelID, sessionID: 's1', state: 'connected'}}))).toBe(false);
    });

    test('getScreenSharingSessionId and isScreenSharingAllowed', () => {
        expect(selectors.getScreenSharingSessionId(makeState(), channelID)).toBe('');
        expect(selectors.getScreenSharingSessionId(makeState({screenSharingIDs: {[channelID]: 's1'}}), channelID)).toBe('s1');
        expect(selectors.isScreenSharingAllowed(makeState())).toBe(true);
        expect(selectors.isScreenSharingAllowed(makeState({callsConfig: {AllowScreenSharing: false}}))).toBe(false);
    });
});

class FakeClient extends EventEmitter {
    channelID = channelID;
    localVideoStream = null;
    mute = jest.fn();
    unmute = jest.fn(() => Promise.resolve());
    raiseHand = jest.fn();
    unraiseHand = jest.fn();
    disconnect = jest.fn();
    unshareScreen = jest.fn();
    shareScreen = jest.fn(() => Promise.resolve(null));
    remoteVideo: Record<string, MediaStream> = {};
    getRemoteVideoStreams = () => this.remoteVideo;
    getRemoteScreenStream = () => null;
    getLocalScreenStream = () => null;
}

const fakeTrack = (id: string) => ({id} as MediaStreamTrack);
const fakeStream = (track: MediaStreamTrack) => ({getVideoTracks: () => [track]} as unknown as MediaStream);

describe('createCallsAPI', () => {
    const originalMediaStream = global.MediaStream;
    let state: GlobalState;
    let store: Store;
    let join: jest.Mock;

    beforeEach(() => {
        // @ts-ignore - jsdom has no MediaStream
        global.MediaStream = jest.fn((tracks: MediaStreamTrack[]) => ({getVideoTracks: () => tracks}));
        state = makeState({localCall: {channelID, sessionID: 's1', state: 'connected'}});
        store = {getState: () => state, dispatch: jest.fn()} as unknown as Store;
        join = jest.fn(() => Promise.resolve());
    });

    afterEach(() => {
        global.MediaStream = originalMediaStream;
        delete window.callsClient;
    });

    test('exposes the contract', () => {
        const {api} = createCallsAPI(store, join);
        expect(api.version).toBe(1);
        expect(Object.keys(api).sort()).toEqual([
            'dismissIncomingCall', 'getLocalScreenStream', 'getLocalVideoStream', 'getRemoteScreenStream',
            'getRemoteVideoStreams', 'host', 'join', 'leave', 'on', 'openSettings', 'selectors', 'setHandRaised',
            'setMuted', 'setVideo', 'startScreenShare', 'stopScreenShare', 'version',
        ]);
        expect(Object.keys(api.selectors).sort()).toEqual([
            'getCall', 'getIncomingCalls', 'getLocalCall', 'getMyMuted', 'getParticipants', 'getScreenSharingSessionId',
            'isCallsEnabled', 'isScreenSharingAllowed', 'isVideoAllowed',
        ]);
        expect(Object.keys(api.host).sort()).toEqual(['lowerHand', 'mute', 'remove', 'stopScreen']);
    });

    test('join goes through the given join function', async () => {
        const {api} = createCallsAPI(store, join);
        await api.join(channelID, {switchCall: true});
        expect(join).toHaveBeenCalledWith(channelID, {switchCall: true});
        await api.join('other');
        expect(join).toHaveBeenCalledWith('other', {});
    });

    test('controls do nothing without a call in this window', () => {
        const {api} = createCallsAPI(store, join);
        api.setMuted(true);
        api.setHandRaised(true);
        api.startScreenShare();
        api.stopScreenShare();
        expect(api.getLocalVideoStream()).toBeNull();
        expect(api.getRemoteVideoStreams()).toEqual({});
    });

    test('change listeners follow each new client', () => {
        const {api, clientCreated, clientClosed} = createCallsAPI(store, join);
        const onChange = jest.fn();
        const unsubscribe = api.on('change', onChange);

        const first = new FakeClient();
        clientCreated(first as unknown as CallsClient);
        expect(onChange).toHaveBeenCalledTimes(1);
        first.emit('connect');
        first.emit('remoteVideoStream');
        expect(onChange).toHaveBeenCalledTimes(3);

        clientClosed();
        expect(onChange).toHaveBeenCalledTimes(4);
        first.emit('mute');
        expect(onChange).toHaveBeenCalledTimes(4);

        const second = new FakeClient();
        clientCreated(second as unknown as CallsClient);
        second.emit('video_on');
        expect(onChange).toHaveBeenCalledTimes(6);

        unsubscribe();
        second.emit('video_off');
        expect(onChange).toHaveBeenCalledTimes(6);
    });

    test('error listeners get the client errors and quality changes', () => {
        const {api, clientCreated, clientClosed} = createCallsAPI(store, join);
        const onError = jest.fn();
        api.on('error', onError);

        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);
        client.emit('error', AudioInputPermissionsError);
        expect(onError).toHaveBeenLastCalledWith({kind: 'mic-permissions', error: AudioInputPermissionsError});

        client.emit('mos', 1);
        expect(onError).toHaveBeenLastCalledWith({kind: 'degraded-quality', mos: 1});
        client.emit('mos', 1.5);
        expect(onError).toHaveBeenCalledTimes(2);
        client.emit('mos', 4.5);
        expect(onError).toHaveBeenLastCalledWith({kind: 'quality-restored', mos: 4.5});

        const err = new Error('boom');
        clientClosed(err);
        expect(onError).toHaveBeenLastCalledWith({kind: 'call-error', error: err});
    });

    test('controls drive the client of this window', () => {
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);

        api.setMuted(false);
        expect(client.unmute).toHaveBeenCalled();
        api.setMuted(true);
        expect(client.mute).toHaveBeenCalled();
        api.setHandRaised(true);
        expect(client.raiseHand).toHaveBeenCalled();
        api.setHandRaised(false);
        expect(client.unraiseHand).toHaveBeenCalled();
        api.stopScreenShare();
        expect(client.unshareScreen).toHaveBeenCalled();
    });

    test('startScreenShare asks for the screen right away', () => {
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);

        api.startScreenShare();
        expect(client.shareScreen).toHaveBeenCalledTimes(1);
    });

    test('startScreenShare does nothing while someone else shares', () => {
        state = makeState({localCall: {channelID, sessionID: 's1', state: 'connected'}, screenSharingIDs: {[channelID]: 's2'}});
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);

        api.startScreenShare();
        expect(client.shareScreen).not.toHaveBeenCalled();
    });

    test('remote video streams keep their reference while their track is the same', () => {
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);

        const t1 = fakeTrack('t1');
        client.remoteVideo = {s1: fakeStream(t1)};
        const streams = api.getRemoteVideoStreams();
        expect(Object.keys(streams)).toEqual(['s1']);

        client.remoteVideo = {s1: fakeStream(t1)};
        expect(api.getRemoteVideoStreams()).toBe(streams);

        client.remoteVideo = {s1: fakeStream(t1), s2: fakeStream(fakeTrack('t2'))};
        const more = api.getRemoteVideoStreams();
        expect(more).not.toBe(streams);
        expect(more.s1).toBe(streams.s1);
    });

    test('leave disconnects the client of this window', () => {
        const {api} = createCallsAPI(store, join);
        const client = new FakeClient();
        window.callsClient = client as unknown as CallsClient;
        api.leave();
        expect(client.disconnect).toHaveBeenCalled();
    });

    test('host controls act on the current call', () => {
        window.callsClient = new FakeClient() as unknown as CallsClient;
        const {api} = createCallsAPI(store, join);
        api.host.mute('s2');
        expect(hostMute).toHaveBeenCalledWith(channelID, 's2');
    });
});
