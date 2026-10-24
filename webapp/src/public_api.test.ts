// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {GlobalState} from '@mattermost/types/store';
import {EventEmitter} from 'events';
import {hostMute, stopRingingForCall} from 'src/actions';
import CallsClient, {AudioInputPermissionsError} from 'src/client';
import RestClient from 'src/rest_client';
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
    stopRingingForCall: jest.fn(() => ({type: 'stop-ringing'})),
}));

jest.mock('src/rest_client', () => ({
    __esModule: true,
    default: {fetch: jest.fn(() => Promise.resolve({})), getOptions: jest.fn()},
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
    ringingForCalls?: Record<string, boolean>;
    modMuted?: Record<string, Record<string, string>>;
    modDeafened?: Record<string, Record<string, string>>;
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
        ringingForCalls: opts.ringingForCalls || {},
        modMuted: opts.modMuted || {},
        modDeafened: opts.modDeafened || {},
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
            {sessionId: 's1', userId: 'owner', muted: false, video: false, screenSharing: true, speaking: true, raisedHand: false, raisedHandAt: 0, isHost: true, isMe: false, modMuted: false, modDeafened: false},
            {sessionId: 's2', userId: currentUserID, muted: true, video: true, screenSharing: false, speaking: false, raisedHand: true, raisedHandAt: 5, isHost: false, isMe: true, modMuted: false, modDeafened: false},
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
        const ringingForCalls = {c1: true};
        const state = makeState({incomingCalls, ringingForCalls});
        const calls = selectors.getIncomingCalls(state);
        expect(calls).toEqual([
            {channelId: 'ch2', callId: 'c2', callerId: 'u2', startAt: 2, type: 'gm', ringing: false},
            {channelId: 'ch1', callId: 'c1', callerId: 'u1', startAt: 1, type: 'dm', ringing: true},
        ]);
        expect(selectors.getIncomingCalls(makeState({incomingCalls, ringingForCalls}))).toBe(calls);

        // The ring stopping gives a new list.
        expect(selectors.getIncomingCalls(makeState({incomingCalls, ringingForCalls: {}}))[1].ringing).toBe(false);
    });

    test('getModMutes and the participants\' modMuted flag follow the moderator mutes', () => {
        expect(selectors.getModMutes(makeState(), channelID)).toEqual([]);
        const modMuted = {[channelID]: {owner: 'admin'}};
        const sessions = {s1: session('s1', 'owner'), s2: session('s2', currentUserID)};
        const state = makeState({sessions, modMuted});
        const mutes = selectors.getModMutes(state, channelID);
        expect(mutes).toEqual([{userId: 'owner', byUserId: 'admin'}]);
        expect(selectors.getModMutes(makeState({modMuted}), channelID)).toBe(mutes);
        expect(selectors.getParticipants(state, channelID).map((p) => p.modMuted)).toEqual([true, false]);
    });

    test('getModDeafens and the participants\' modDeafened flag follow the moderator deafens', () => {
        expect(selectors.getModDeafens!(makeState(), channelID)).toEqual([]);
        const modDeafened = {[channelID]: {[currentUserID]: 'owner'}};
        const sessions = {s1: session('s1', 'owner'), s2: session('s2', currentUserID)};
        const state = makeState({sessions, modDeafened});
        const deafens = selectors.getModDeafens!(state, channelID);
        expect(deafens).toEqual([{userId: currentUserID, byUserId: 'owner'}]);
        expect(selectors.getModDeafens!(makeState({modDeafened}), channelID)).toBe(deafens);
        expect(selectors.getModMutes(state, channelID)).toEqual([]);
        const participants = selectors.getParticipants(state, channelID);
        expect(participants.map((p) => p.modDeafened)).toEqual([false, true]);
        expect(participants.map((p) => p.modMuted)).toEqual([false, false]);
    });

    test('getReactions maps the call\'s reactions and keeps the result while unchanged', () => {
        expect(selectors.getReactions(makeState(), channelID)).toEqual([]);
        const reactions = [{user_id: 'u1', session_id: 's1', emoji: {name: '+1', unified: '1f44d'}, timestamp: 42, displayName: 'U'}];
        const state = makeState();
        (state as any)['plugins-com.mattermost.calls'].reactions = {[channelID]: {reactions}};
        const result = selectors.getReactions(state, channelID);
        expect(result).toEqual([{sessionId: 's1', userId: 'u1', emojiName: '+1', unified: '1f44d', timestamp: 42}]);
        expect(selectors.getReactions(state, channelID)).toBe(result);
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
    getSessionID = () => 's1';
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
        store = {getState: () => state, dispatch: jest.fn(), subscribe: jest.fn(() => () => {})} as unknown as Store;
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
            'canShareSystemAudio', 'dismissIncomingCall', 'getBackgroundBlur', 'getLocalScreenStream', 'getLocalVideoStream', 'getParticipantVolume',
            'getRemoteScreenStream', 'getRemoteVideoStreams', 'host', 'join', 'leave', 'moderation', 'on', 'openSettings', 'selectors',
            'sendReaction', 'setBackgroundBlur', 'setHandRaised', 'setMuted', 'setParticipantVolume', 'setVideo',
            'startScreenShare', 'startTranscription', 'stopRinging', 'stopScreenShare', 'stopTranscription', 'version',
        ]);
        expect(Object.keys(api.selectors).sort()).toEqual([
            'getCall', 'getIncomingCalls', 'getLiveCaptions', 'getLocalCall', 'getModDeafens', 'getModMutes', 'getMyMuted', 'getParticipants', 'getReactions',
            'getScreenSharingSessionId', 'getTranscription', 'isCallsEnabled', 'isScreenSharingAllowed', 'isTranscriptionAvailable',
            'isVideoAllowed',
        ]);
        expect(Object.keys(api.host).sort()).toEqual(['lowerHand', 'mute', 'remove', 'stopScreen']);
        expect(Object.keys(api.moderation).sort()).toEqual(['askToUnmute', 'listModMutes', 'move', 'participantPermissions', 'setModDeafened', 'setModMuted']);
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

    test('startScreenShare asks for the kind of source chosen', () => {
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);

        api.startScreenShare({height: 1080, frameRate: 30}, 'window');
        expect(client.shareScreen).toHaveBeenCalledWith('', expect.anything(), {height: 1080, frameRate: 30}, 'window');
    });

    describe('in the desktop app', () => {
        afterEach(() => {
            delete window.desktop;
            window.localStorage.clear();
        });

        test('startScreenShare shares the source picked by the caller', () => {
            window.desktop = {version: '6.0.0'};
            const {api, clientCreated} = createCallsAPI(store, join);
            const client = new FakeClient();
            clientCreated(client as unknown as CallsClient);

            api.startScreenShare({height: 720, frameRate: 15}, undefined, {desktopSourceId: 'screen:1:0', withAudio: true});
            expect(client.shareScreen).toHaveBeenCalledWith('screen:1:0', true, {height: 720, frameRate: 15}, undefined);
            expect(store.dispatch).not.toHaveBeenCalledWith({type: 'screen-source'});
        });

        test('startScreenShare leaves the system audio out unless asked for and allowed', () => {
            window.desktop = {version: '6.0.0'};
            const {api, clientCreated} = createCallsAPI(store, join);
            const client = new FakeClient();
            clientCreated(client as unknown as CallsClient);

            api.startScreenShare(undefined, undefined, {desktopSourceId: 'window:2:0'});
            expect(client.shareScreen).toHaveBeenLastCalledWith('window:2:0', false, undefined, undefined);

            window.localStorage.setItem('calls_share_audio_with_screen', 'off');
            api.startScreenShare(undefined, undefined, {desktopSourceId: 'window:2:0', withAudio: true});
            expect(client.shareScreen).toHaveBeenLastCalledWith('window:2:0', false, undefined, undefined);
        });

        test('startScreenShare opens Calls\' picker without a source', () => {
            window.desktop = {version: '6.0.0'};
            const {api, clientCreated} = createCallsAPI(store, join);
            const client = new FakeClient();
            clientCreated(client as unknown as CallsClient);

            api.startScreenShare();
            expect(client.shareScreen).not.toHaveBeenCalled();
            expect(store.dispatch).toHaveBeenCalledWith({type: 'screen-source'});
        });

        test('canShareSystemAudio', () => {
            const {api} = createCallsAPI(store, join);
            expect(api.canShareSystemAudio()).toBe(false);

            window.desktop = {version: '6.0.0'};
            const platform = jest.spyOn(window.navigator, 'platform', 'get');
            platform.mockReturnValue('Linux x86_64');
            expect(api.canShareSystemAudio()).toBe(true);
            window.localStorage.setItem('calls_share_audio_with_screen', 'off');
            expect(api.canShareSystemAudio()).toBe(false);
            window.localStorage.clear();
            platform.mockReturnValue('MacIntel');
            expect(api.canShareSystemAudio()).toBe(false);
            platform.mockRestore();
        });
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

    test('host controls act on the given call', () => {
        const {api} = createCallsAPI(store, join);
        api.host.mute('s3', 'other-channel');
        expect(hostMute).toHaveBeenCalledWith('other-channel', 's3');
    });

    test('unmuting is refused while muted by a moderator', () => {
        state = makeState({localCall: {channelID, sessionID: 's1', state: 'connected'}, modMuted: {[channelID]: {[currentUserID]: 'admin'}}});
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);
        const onError = jest.fn();
        api.on('error', onError);

        api.setMuted(false);
        expect(client.unmute).not.toHaveBeenCalled();
        expect(onError).toHaveBeenCalledWith({kind: 'mod-muted'});
    });

    test('moderation requests go to the host controls and reject with the status', async () => {
        const fetch = RestClient.fetch as jest.Mock;
        const {api} = createCallsAPI(store, join);

        await api.moderation.setModMuted(channelID, 'u2', true);
        expect(fetch).toHaveBeenLastCalledWith(expect.stringMatching(`/calls/${channelID}/host/mod-mute$`), {method: 'post', body: JSON.stringify({user_id: 'u2', muted: true})});

        await api.moderation.askToUnmute(channelID, 's2');
        expect(fetch).toHaveBeenLastCalledWith(expect.stringMatching(`/calls/${channelID}/host/ask-unmute$`), {method: 'post', body: JSON.stringify({session_id: 's2'})});

        fetch.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('too many requests'), {status_code: 429})));
        await expect(api.moderation.askToUnmute(channelID, 's2')).rejects.toMatchObject({status: 429});

        await api.moderation.setModDeafened!(channelID, 'u2', true);
        expect(fetch).toHaveBeenLastCalledWith(expect.stringMatching(`/calls/${channelID}/host/mod-deafen$`), {method: 'post', body: JSON.stringify({user_id: 'u2', deafened: true})});

        await api.moderation.move!(channelID, 's2', 'voice2');
        expect(fetch).toHaveBeenLastCalledWith(expect.stringMatching(`/calls/${channelID}/host/move$`), {method: 'post', body: JSON.stringify({session_id: 's2', channel_id: 'voice2'})});

        fetch.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('they can\'t join that channel'), {status_code: 400})));
        await expect(api.moderation.move!(channelID, 's2', 'voice2')).rejects.toMatchObject({status: 400});
    });

    test('moderators list the lasting mutes and deafens of a channel', async () => {
        const fetch = RestClient.fetch as jest.Mock;
        const {api} = createCallsAPI(store, join);

        fetch.mockImplementationOnce(() => Promise.resolve([
            {user_id: 'u2', deafened_by: 'admin', deafened_at: 20, can_lift_mute: false, can_lift_deafen: false},
            {user_id: 'u3', muted_by: 'mod', muted_at: 10, can_lift_mute: true, can_lift_deafen: false},
        ]));
        await expect(api.moderation.listModMutes!(channelID)).resolves.toEqual([
            {userId: 'u2', mutedBy: '', mutedAt: 0, deafenedBy: 'admin', deafenedAt: 20, canLiftMute: false, canLiftDeafen: false},
            {userId: 'u3', mutedBy: 'mod', mutedAt: 10, deafenedBy: '', deafenedAt: 0, canLiftMute: true, canLiftDeafen: false},
        ]);
        expect(fetch).toHaveBeenLastCalledWith(expect.stringMatching(`/calls/${channelID}/host/mod-mutes$`), {method: 'get'});

        fetch.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('no permissions'), {status_code: 403})));
        await expect(api.moderation.listModMutes!(channelID)).rejects.toMatchObject({status: 403});
    });

    test('the participant menu asks what the user may do to someone', async () => {
        const fetch = RestClient.fetch as jest.Mock;
        const {api} = createCallsAPI(store, join);

        fetch.mockImplementationOnce(() => Promise.resolve({session_id: 's2', user_id: 'u2', can_mute: true, can_deafen: false, can_move: false, can_kick: true}));
        await expect(api.moderation.participantPermissions!(channelID, 's2')).resolves.toEqual({
            sessionId: 's2',
            canMute: true,
            canDeafen: false,
            canMove: false,
            canKick: true,
        });
        expect(fetch).toHaveBeenLastCalledWith(expect.stringMatching(`/calls/${channelID}/host/permissions\\?session_id=s2$`), {method: 'get'});

        fetch.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('not in call'), {status_code: 400})));
        await expect(api.moderation.participantPermissions!(channelID, 's9')).rejects.toMatchObject({status: 400});
    });

    test('unmuting is refused while deafened by a moderator', () => {
        state = makeState({localCall: {channelID, sessionID: 's1', state: 'connected'}, modDeafened: {[channelID]: {[currentUserID]: 'admin'}}});
        const {api, clientCreated} = createCallsAPI(store, join);
        const client = new FakeClient();
        clientCreated(client as unknown as CallsClient);
        const onError = jest.fn();
        api.on('error', onError);

        api.setMuted(false);
        expect(client.unmute).not.toHaveBeenCalled();
        expect(onError).toHaveBeenCalledWith({kind: 'mod-deafened'});
    });

    test('a move switches the call of this window to the other channel, and tells the listeners', async () => {
        state = makeState({localCall: {channelID, sessionID: 's1', state: 'connected'}});
        const {api, moveRequested} = createCallsAPI(store, join);
        const onMove = jest.fn();
        api.on('move', onMove);
        const request = {channelId: channelID, sessionId: 's1', toChannelId: 'voice2', byUserId: 'owner'};

        await moveRequested(request);
        expect(onMove).toHaveBeenCalledWith(request);
        expect(join).toHaveBeenCalledWith('voice2', {switchCall: true});

        // Not for another session or call: another tab of the user's, say.
        onMove.mockClear();
        (join as jest.Mock).mockClear();
        await moveRequested({...request, sessionId: 's9'});
        await moveRequested({...request, channelId: 'other'});
        state = makeState();
        await moveRequested(request);
        expect(onMove).not.toHaveBeenCalled();
        expect(join).not.toHaveBeenCalled();

        // The desktop app's main window switches the call of its call window.
        state = makeState({clientStateReducer: {channelID, sessionID: 's1'}});
        await moveRequested(request);
        expect(join).toHaveBeenCalledWith('voice2', {switchCall: true});
    });

    test('stopRinging stops the ring of an incoming call', () => {
        const {api} = createCallsAPI(store, join);
        api.stopRinging('call-id');
        expect(stopRingingForCall).toHaveBeenCalledWith('call-id');
        expect(store.dispatch).toHaveBeenCalledWith({type: 'stop-ringing'});
    });

    test('unmute requests reach the listeners', () => {
        const {api, unmuteRequested} = createCallsAPI(store, join);
        const onRequest = jest.fn();
        const off = api.on('unmute-request', onRequest);
        const request = {channelId: channelID, sessionId: 's1', byUserId: 'owner'};
        unmuteRequested(request);
        expect(onRequest).toHaveBeenCalledWith(request);
        off();
        unmuteRequested(request);
        expect(onRequest).toHaveBeenCalledTimes(1);
    });
});
