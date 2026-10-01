// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {mosThreshold} from '@mattermost/calls-common';
import {UserSessionState} from '@mattermost/calls-common/lib/types';
import {GlobalState} from '@mattermost/types/store';
import {getChannel} from 'mattermost-redux/selectors/entities/channels';
import {isCurrentUserSystemAdmin} from 'mattermost-redux/selectors/entities/users';
import {compareSemVer} from 'semver-parser';
import {
    dismissIncomingCallNotification,
    hostLowerHand,
    hostMute,
    hostRemove,
    hostScreenOff,
    openCallsUserSettings,
    showScreenSourceModal,
} from 'src/actions';
import CallsClient, {
    AudioInputMissingError,
    AudioInputPermissionsError,
    VideoInputMissingError,
    VideoInputPermissionsError,
} from 'src/client';
import {logErr} from 'src/log';
import {
    allowScreenSharing,
    callsExplicitlyDisabled,
    callsExplicitlyEnabled,
    channelHasCall,
    channelIDForCurrentCall,
    defaultEnabled,
    pluginState,
    videoEnabledInChannel,
} from 'src/selectors';
import type {
    AntimatterCallsAPI,
    CallsCall,
    CallsErrorEvent,
    CallsEvent,
    CallsIncomingCall,
    CallsJoinOptions,
    CallsParticipant,
    LocalCall,
} from 'src/types/public_api';
import {CallJobReduxState, ChannelType, IncomingCallNotification} from 'src/types/types';
import {sendDesktopEvent, shareAudioWithScreen, shouldRenderDesktopWidget} from 'src/utils';

import {Store} from './types/antimatter-webapp';

//
// Selectors
//

type CallRef = {channelID: string; sessionID: string};

// myCall returns the user's call: the one run by this window, or by the desktop app's call window.
function myCall(state: GlobalState): CallRef | null {
    return pluginState(state).localCall || pluginState(state).clientStateReducer || null;
}

function isCallsEnabled(state: GlobalState, channelId: string): boolean {
    // Anyone can join a call in progress, the rest follows joinCall's checks.
    if (channelHasCall(state, channelId)) {
        return true;
    }
    if (callsExplicitlyDisabled(state, channelId)) {
        return false;
    }
    return callsExplicitlyEnabled(state, channelId) || Boolean(defaultEnabled(state)) || isCurrentUserSystemAdmin(state);
}

type CallCacheEntry = {
    call: unknown;
    host: unknown;
    recording: unknown;
    result: CallsCall;
};

const callCache = new Map<string, CallCacheEntry>();

function getCall(state: GlobalState, channelId: string): CallsCall | null {
    const calls = pluginState(state);
    const call = calls.calls?.[channelId];
    if (!call) {
        return null;
    }
    const host = calls.hosts?.[channelId];
    const recording: CallJobReduxState | undefined = calls.recordings?.[channelId];

    const cached = callCache.get(channelId);
    if (cached && cached.call === call && cached.host === host && cached.recording === recording) {
        return cached.result;
    }

    const result: CallsCall = {
        channelId,
        id: call.ID,
        startAt: call.startAt,
        ownerId: call.ownerID,
        hostId: host?.hostID || '',
        threadId: call.threadID || '',
        recording: Boolean(recording && recording.init_at > recording.end_at),
    };
    callCache.set(channelId, {call, host, recording, result});
    return result;
}

type ParticipantsCacheEntry = {
    sessions: unknown;
    screenSharingId: string;
    hostId: string;
    mySessionId: string;
    result: CallsParticipant[];
};

const participantsCache = new Map<string, ParticipantsCacheEntry>();
const noParticipants: CallsParticipant[] = [];

function getParticipants(state: GlobalState, channelId: string): CallsParticipant[] {
    const calls = pluginState(state);
    const sessions: Record<string, UserSessionState> | undefined = calls.sessions?.[channelId];
    const screenSharingId: string = calls.screenSharingIDs?.[channelId] || '';
    const hostId: string = calls.hosts?.[channelId]?.hostID || '';
    const me = myCall(state);
    const mySessionId = me?.channelID === channelId ? me.sessionID : '';

    const cached = participantsCache.get(channelId);
    if (cached && cached.sessions === sessions && cached.screenSharingId === screenSharingId &&
        cached.hostId === hostId && cached.mySessionId === mySessionId) {
        return cached.result;
    }

    let result = noParticipants;
    if (sessions && Object.keys(sessions).length) {
        result = Object.values(sessions).map((session) => ({
            sessionId: session.session_id,
            userId: session.user_id,
            muted: !session.unmuted,
            video: Boolean(session.video),
            screenSharing: session.session_id === screenSharingId,
            speaking: Boolean(session.voice) && session.unmuted,
            raisedHand: session.raised_hand > 0,
            isHost: Boolean(hostId) && session.user_id === hostId,
            isMe: Boolean(mySessionId) && session.session_id === mySessionId,
        }));
    }
    participantsCache.set(channelId, {sessions, screenSharingId, hostId, mySessionId, result});
    return result;
}

let localCallCache: {local: unknown; desktop: unknown; result: LocalCall | null} = {local: null, desktop: null, result: null};

function getLocalCall(state: GlobalState): LocalCall | null {
    const local = pluginState(state).localCall || null;
    const desktop = pluginState(state).clientStateReducer || null;
    if (localCallCache.local === local && localCallCache.desktop === desktop) {
        return localCallCache.result;
    }

    let result: LocalCall | null = null;
    if (local) {
        result = {channelId: local.channelID, sessionId: local.sessionID, state: local.state, mediaInThisWindow: true};
    } else if (desktop) {
        // The desktop app runs the call in its own window.
        result = {channelId: desktop.channelID, sessionId: desktop.sessionID, state: 'connected', mediaInThisWindow: false};
    }
    localCallCache = {local, desktop, result};
    return result;
}

function getScreenSharingSessionId(state: GlobalState, channelId: string): string {
    return pluginState(state).screenSharingIDs?.[channelId] || '';
}

let incomingCallsCache: {calls: unknown; result: CallsIncomingCall[]} = {calls: null, result: []};

function getIncomingCalls(state: GlobalState): CallsIncomingCall[] {
    const calls: IncomingCallNotification[] = pluginState(state).incomingCalls || [];
    if (incomingCallsCache.calls === calls) {
        return incomingCallsCache.result;
    }

    const result = [...calls].sort((a, b) => b.startAt - a.startAt).map((call) => ({
        channelId: call.channelID,
        callId: call.callID,
        callerId: call.callerID,
        startAt: call.startAt,
        type: call.type === ChannelType.DM ? 'dm' as const : 'gm' as const,
    }));
    incomingCallsCache = {calls, result};
    return result;
}

function isVideoAllowed(state: GlobalState, channelId: string): boolean {
    return videoEnabledInChannel(state, getChannel(state, channelId));
}

function isScreenSharingAllowed(state: GlobalState): boolean {
    return Boolean(allowScreenSharing(state));
}

function getMyMuted(state: GlobalState): boolean {
    const me = myCall(state);
    if (!me?.sessionID) {
        return true;
    }
    return !pluginState(state).sessions?.[me.channelID]?.[me.sessionID]?.unmuted;
}

export const selectors = {
    isCallsEnabled,
    getCall,
    getParticipants,
    getLocalCall,
    getScreenSharingSessionId,
    getIncomingCalls,
    isVideoAllowed,
    isScreenSharingAllowed,
    getMyMuted,
} as AntimatterCallsAPI['selectors'];

//
// API
//

// The calls client events after which the call's media or controls may have changed.
const changeEvents = [
    'connect',
    'mute',
    'unmute',
    'raise_hand',
    'lower_hand',
    'video_on',
    'video_off',
    'localVideoStream',
    'localScreenStream',
    'remoteVideoStream',
    'remoteScreenStream',
    'devicechange',
    'initaudio',
    'initvideo',
];

function errorEvent(error: Error): CallsErrorEvent {
    switch (error) {
    case AudioInputPermissionsError:
        return {kind: 'mic-permissions', error};
    case AudioInputMissingError:
        return {kind: 'mic-missing', error};
    case VideoInputPermissionsError:
        return {kind: 'camera-permissions', error};
    case VideoInputMissingError:
        return {kind: 'camera-missing', error};
    default:
        return {kind: 'other', error};
    }
}

function sameStreams(a: Record<string, MediaStream>, b: Record<string, MediaStream>) {
    const keys = Object.keys(a);
    return keys.length === Object.keys(b).length && keys.every((key) => a[key] === b[key]);
}

export type JoinCall = (channelId: string, opts: CallsJoinOptions) => Promise<void>;

export type CallsAPIHandle = {
    api: AntimatterCallsAPI;

    // To be called when this window creates a calls client, and when it's closed.
    clientCreated(client: CallsClient): void;
    clientClosed(err?: Error): void;
};

// createCallsAPI creates window.antimatterCalls. Joins go through joinCall, so that they get the
// same checks as Calls' own UI.
export function createCallsAPI(store: Store, joinCall: JoinCall): CallsAPIHandle {
    const listeners: Record<CallsEvent, Set<(...args: any[]) => void>> = {
        change: new Set(),
        error: new Set(),
    };

    const emit = (event: CallsEvent, ...args: unknown[]) => {
        for (const listener of [...listeners[event]]) {
            try {
                listener(...args);
            } catch (err) {
                logErr(err);
            }
        }
    };

    let client: CallsClient | null = null;
    let degradedQuality = false;

    // The client builds new streams on every call: keep one per track so that <video> elements
    // don't restart.
    let streamsByTrack = new Map<string, MediaStream>();
    let remoteVideoStreams: Record<string, MediaStream> = {};

    const streamFor = (track?: MediaStreamTrack) => {
        if (!track) {
            return null;
        }
        let stream = streamsByTrack.get(track.id);
        if (!stream) {
            stream = new MediaStream([track]);
            streamsByTrack.set(track.id, stream);
        }
        return stream;
    };

    const onChange = () => emit('change');
    const onError = (err: Error) => emit('error', errorEvent(err));
    const onInitAudio = () => emit('error', {kind: 'mic-ready'});
    const onInitVideo = () => emit('error', {kind: 'camera-ready'});
    const onDeviceFallback = (device: MediaDeviceInfo) => emit('error', {kind: 'device-fallback', device});
    const onMOS = (mos: number) => {
        if (!degradedQuality && mos < mosThreshold) {
            degradedQuality = true;
            emit('error', {kind: 'degraded-quality', mos});
        } else if (degradedQuality && mos >= mosThreshold) {
            degradedQuality = false;
            emit('error', {kind: 'quality-restored', mos});
        }
    };

    const detach = () => {
        if (client) {
            for (const event of changeEvents) {
                client.off(event, onChange);
            }
            client.off('error', onError);
            client.off('initaudio', onInitAudio);
            client.off('initvideo', onInitVideo);
            client.off('devicefallback', onDeviceFallback);
            client.off('mos', onMOS);
        }
        client = null;
        degradedQuality = false;
        streamsByTrack = new Map();
        remoteVideoStreams = {};
    };

    const clientCreated = (newClient: CallsClient) => {
        detach();
        client = newClient;
        for (const event of changeEvents) {
            client.on(event, onChange);
        }
        client.on('error', onError);
        client.on('initaudio', onInitAudio);
        client.on('initvideo', onInitVideo);
        client.on('devicefallback', onDeviceFallback);
        client.on('mos', onMOS);
        emit('change');
    };

    const clientClosed = (err?: Error) => {
        detach();
        if (err) {
            emit('error', {kind: 'call-error', error: err});
        }
        emit('change');
    };

    const hostAction = (action: (callID: string, sessionID: string) => Promise<unknown>) => (sessionId: string) => {
        const channelID = channelIDForCurrentCall(store.getState());
        if (channelID) {
            action(channelID, sessionId).catch((err) => logErr(err));
        }
    };

    const api: AntimatterCallsAPI = {
        version: 1,
        selectors,

        join: (channelId, opts = {}) => joinCall(channelId, opts),

        leave: () => {
            if (window.callsClient) {
                window.callsClient.disconnect();
                return;
            }

            // The desktop app may run the call in its own window.
            const channelID = channelIDForCurrentCall(store.getState());
            if (!channelID) {
                return;
            }
            if (window.desktopAPI?.leaveCall) {
                window.desktopAPI.leaveCall();
            } else if (shouldRenderDesktopWidget()) {
                // DEPRECATED: legacy Desktop API logic (<= 5.6.0)
                sendDesktopEvent('calls-leave-call', {callID: channelID});
            }
        },

        setMuted: (muted) => {
            if (!client) {
                return;
            }
            if (muted) {
                client.mute();
            } else {
                client.unmute().catch((err) => logErr(err));
            }
        },

        setVideo: async (on) => {
            if (!client) {
                return;
            }
            if (on) {
                await client.startVideo();
            } else {
                client.stopVideo();
            }
        },

        startScreenShare: () => {
            const state = store.getState();
            if (!client || !allowScreenSharing(state) || getScreenSharingSessionId(state, client.channelID)) {
                return;
            }

            // The desktop app needs the user to pick a screen or window first.
            if (window.desktop && compareSemVer(window.desktop.version, '5.1.0') >= 0) {
                store.dispatch(showScreenSourceModal());
                return;
            }

            // This must run synchronously from the click: the browser only lets a page ask to
            // capture the screen right after a user action.
            client.shareScreen('', shareAudioWithScreen()).then((stream) => {
                if (!stream) {
                    emit('error', {kind: 'screen-share-failed'});
                    return;
                }

                // Sharing can be stopped from the browser's own controls.
                stream.getVideoTracks()[0]?.addEventListener('ended', onChange, {once: true});
            }).catch((err) => logErr(err));
        },

        stopScreenShare: () => {
            if (!client) {
                return;
            }
            client.unshareScreen();
            emit('change');
        },

        setHandRaised: (raised) => {
            if (!client) {
                return;
            }
            if (raised) {
                client.raiseHand();
            } else {
                client.unraiseHand();
            }
        },

        getLocalVideoStream: () => client?.localVideoStream || null,

        getLocalScreenStream: () => streamFor(client?.getLocalScreenStream()?.getVideoTracks()[0]),

        getRemoteVideoStreams: () => {
            const streams: Record<string, MediaStream> = {};
            for (const [sessionID, stream] of Object.entries(client?.getRemoteVideoStreams() || {})) {
                const stable = streamFor(stream.getVideoTracks()[0]);
                if (stable) {
                    streams[sessionID] = stable;
                }
            }
            if (!sameStreams(streams, remoteVideoStreams)) {
                remoteVideoStreams = streams;
            }
            return remoteVideoStreams;
        },

        getRemoteScreenStream: () => streamFor(client?.getRemoteScreenStream()?.getVideoTracks()[0]),

        on: (event: CallsEvent, cb: (...args: any[]) => void) => {
            if (!listeners[event]) {
                logErr('antimatterCalls.on: unknown event', event);
                return () => undefined;
            }
            listeners[event].add(cb);
            return () => {
                listeners[event].delete(cb);
            };
        },

        host: {
            mute: hostAction(hostMute),
            remove: hostAction(hostRemove),
            lowerHand: hostAction(hostLowerHand),
            stopScreen: hostAction(hostScreenOff),
        },

        dismissIncomingCall: (channelId, callId) => {
            store.dispatch(dismissIncomingCallNotification(channelId, callId));
        },

        openSettings: () => {
            store.dispatch(openCallsUserSettings());
        },
    };

    return {api, clientCreated, clientClosed};
}
