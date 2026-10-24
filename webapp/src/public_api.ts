// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {mosThreshold} from '@mattermost/calls-common';
import {Reaction, UserSessionState} from '@mattermost/calls-common/lib/types';
import {GlobalState} from '@mattermost/types/store';
import {getChannel} from 'mattermost-redux/selectors/entities/channels';
import {getCurrentUserId, isCurrentUserSystemAdmin} from 'mattermost-redux/selectors/entities/users';
import {compareSemVer} from 'semver-parser';
import {
    dismissIncomingCallNotification,
    hostLowerHand,
    hostMute,
    hostRemove,
    hostScreenOff,
    openCallsUserSettings,
    ParticipantPermissionsData,
    showScreenSourceModal,
    stopRingingForCall,
} from 'src/actions';
import {getSessionVolume, setSessionVolume} from 'src/call_audio';
import CallsClient, {
    AudioInputMissingError,
    AudioInputPermissionsError,
    VideoInputMissingError,
    VideoInputPermissionsError,
} from 'src/client';
import {getBgBlurData, setBgBlurData} from 'src/local_storage';
import {logErr} from 'src/log';
import RestClient from 'src/rest_client';
import {
    allowScreenSharing,
    callsExplicitlyDisabled,
    callsExplicitlyEnabled,
    channelHasCall,
    channelIDForCurrentCall,
    defaultEnabled,
    isJobRunning,
    isModDeafened,
    isModMuted,
    modDeafenedInChannel,
    modMutedInChannel,
    pluginState,
    ringingForCall,
    transcribeFromClients,
    transcriptionStateInChannel,
    videoEnabledInChannel,
} from 'src/selectors';
import type {
    AntimatterCallsAPI,
    CallsCall,
    CallsCaption,
    CallsErrorEvent,
    CallsEvent,
    CallsIncomingCall,
    CallsJoinOptions,
    CallsLastingMute,
    CallsModMute,
    CallsMoveRequest,
    CallsParticipant,
    CallsParticipantPermissions,
    CallsReaction,
    CallsTranscription,
    CallsUnmuteRequest,
    LocalCall,
} from 'src/types/public_api';
import {CallJobReduxState, ChannelType, IncomingCallNotification, LiveCaptions} from 'src/types/types';
import {getPlatformInfo, getPluginPath, sendDesktopEvent, shareAudioWithScreen, shouldRenderDesktopWidget} from 'src/utils';
import UtteranceTranscriber from 'src/utterance_transcriber';

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
    modMuted: unknown;
    modDeafened: unknown;
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
    const modMuted = modMutedInChannel(state, channelId);
    const modDeafened = modDeafenedInChannel(state, channelId);

    const cached = participantsCache.get(channelId);
    if (cached && cached.sessions === sessions && cached.modMuted === modMuted && cached.modDeafened === modDeafened &&
        cached.screenSharingId === screenSharingId &&
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
            raisedHandAt: session.raised_hand || 0,
            isHost: Boolean(hostId) && session.user_id === hostId,
            isMe: Boolean(mySessionId) && session.session_id === mySessionId,
            modMuted: Object.hasOwn(modMuted, session.user_id),
            modDeafened: Object.hasOwn(modDeafened, session.user_id),
        }));
    }
    participantsCache.set(channelId, {sessions, modMuted, modDeafened, screenSharingId, hostId, mySessionId, result});
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

let incomingCallsCache: {calls: unknown; ringing: unknown; result: CallsIncomingCall[]} = {calls: null, ringing: null, result: []};

function getIncomingCalls(state: GlobalState): CallsIncomingCall[] {
    const calls: IncomingCallNotification[] = pluginState(state).incomingCalls || [];
    const ringing = pluginState(state).ringingForCalls;
    if (incomingCallsCache.calls === calls && incomingCallsCache.ringing === ringing) {
        return incomingCallsCache.result;
    }

    const result = [...calls].sort((a, b) => b.startAt - a.startAt).map((call) => ({
        channelId: call.channelID,
        callId: call.callID,
        callerId: call.callerID,
        startAt: call.startAt,
        type: call.type === ChannelType.DM ? 'dm' as const : 'gm' as const,
        ringing: ringingForCall(state, call.callID),
    }));
    incomingCallsCache = {calls, ringing, result};
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

let reactionsCache: {reactions: unknown; result: CallsReaction[]} = {reactions: null, result: []};

function getReactions(state: GlobalState, channelId: string): CallsReaction[] {
    const reactions: Reaction[] = pluginState(state).reactions?.[channelId]?.reactions || [];
    if (reactionsCache.reactions === reactions) {
        return reactionsCache.result;
    }
    const result = reactions.map((r) => ({
        sessionId: r.session_id,
        userId: r.user_id,
        emojiName: r.emoji?.name || '',
        unified: r.emoji?.unified || '',
        timestamp: r.timestamp,
    }));
    reactionsCache = {reactions, result};
    return result;
}

function isTranscriptionAvailable(state: GlobalState): boolean {
    return transcribeFromClients(state);
}

const noTranscription: CallsTranscription = {running: false, startAt: 0, liveCaptions: false};
let transcriptionCache: {job: unknown; captions: unknown; result: CallsTranscription} = {job: null, captions: null, result: noTranscription};

function getTranscription(state: GlobalState, channelId: string): CallsTranscription {
    const job = transcriptionStateInChannel(state, channelId);
    if (!isJobRunning(job)) {
        return noTranscription;
    }
    const captions = pluginState(state).callLiveCaptionsState?.[channelId];
    if (transcriptionCache.job === job && transcriptionCache.captions === captions) {
        return transcriptionCache.result;
    }
    const result = {running: true, startAt: job!.start_at, liveCaptions: isJobRunning(captions)};
    transcriptionCache = {job, captions, result};
    return result;
}

let captionsCache: {captions: unknown; result: CallsCaption[]} = {captions: null, result: []};

function getLiveCaptions(state: GlobalState, channelId: string): CallsCaption[] {
    const captions = pluginState(state).liveCaptions?.[channelId];
    if (captionsCache.captions === captions) {
        return captionsCache.result;
    }
    const result = Object.values((captions || {}) as LiveCaptions).map((c) => ({
        sessionId: c.session_id,
        userId: c.user_id,
        text: c.text,
        captionId: c.caption_id,
    }));
    captionsCache = {captions, result};
    return result;
}

const noModMutes: CallsModMute[] = [];

// modListSelector lists the people of a channel that a moderator muted (or deafened), from who -> by whom.
function modListSelector(byChannel: (state: GlobalState, channelId: string) => Record<string, string>) {
    const cache = new Map<string, {users: unknown; result: CallsModMute[]}>();
    return (state: GlobalState, channelId: string): CallsModMute[] => {
        const users = byChannel(state, channelId);
        const cached = cache.get(channelId);
        if (cached && cached.users === users) {
            return cached.result;
        }
        const entries = Object.entries(users);
        const result = entries.length ? entries.map(([userId, byUserId]) => ({userId, byUserId})) : noModMutes;
        cache.set(channelId, {users, result});
        return result;
    };
}

const getModMutes = modListSelector(modMutedInChannel);
const getModDeafens = modListSelector(modDeafenedInChannel);

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
    getReactions,
    isTranscriptionAvailable,
    getTranscription,
    getLiveCaptions,
    getModMutes,
    getModDeafens,
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

    // To be called when a moderator asks the user to unmute in the call of this window.
    unmuteRequested(request: CallsUnmuteRequest): void;

    // To be called when a moderator moves the user to another voice channel (any window of theirs).
    moveRequested(request: CallsMoveRequest): Promise<void>;
};

// The moderator mutes of a channel as the server lists them.
type LastingMuteData = {
    user_id: string;
    muted_by?: string;
    muted_at?: number;
    deafened_by?: string;
    deafened_at?: number;
    can_lift_mute: boolean;
    can_lift_deafen: boolean;
};

function lastingMute(d: LastingMuteData): CallsLastingMute {
    return {
        userId: d.user_id,
        mutedBy: d.muted_by || '',
        mutedAt: d.muted_at || 0,
        deafenedBy: d.deafened_by || '',
        deafenedAt: d.deafened_at || 0,
        canLiftMute: Boolean(d.can_lift_mute),
        canLiftDeafen: Boolean(d.can_lift_deafen),
    };
}

function participantPermissions(d: ParticipantPermissionsData): CallsParticipantPermissions {
    return {
        sessionId: d.session_id,
        canMute: Boolean(d.can_mute),
        canDeafen: Boolean(d.can_deafen),
        canMove: Boolean(d.can_move),
        canKick: Boolean(d.can_kick),
    };
}

// createCallsAPI creates window.antimatterCalls. Joins go through joinCall, so that they get the
// same checks as Calls' own UI.
export function createCallsAPI(store: Store, joinCall: JoinCall): CallsAPIHandle {
    const listeners: Record<CallsEvent, Set<(...args: any[]) => void>> = {
        change: new Set(),
        error: new Set(),
        'unmute-request': new Set(),
        move: new Set(),
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

    // While the call of this window is transcribed from the participants' audio, the transcriber sends ours.
    let transcriber: UtteranceTranscriber | null = null;
    const syncTranscriber = () => {
        const state = store.getState();
        const channelID = channelIDForCurrentCall(state);
        const running = Boolean(client && channelID && client.getSessionID() && transcribeFromClients(state) &&
            isJobRunning(transcriptionStateInChannel(state, channelID)));
        if (!running) {
            transcriber?.stop();
            transcriber = null;
            return;
        }
        if (!transcriber) {
            transcriber = new UtteranceTranscriber(async (audio, startAt, sentAt) => {
                const sessionID = client?.getSessionID();
                if (!sessionID) {
                    return;
                }
                const form = new FormData();
                form.append('audio', audio, 'utterance.wav');
                form.append('session_id', sessionID);
                form.append('start_at', String(startAt));
                form.append('sent_at', String(sentAt));
                const options = RestClient.getOptions({method: 'post', body: form});
                const res = await fetch(`${getPluginPath()}/calls/${channelID}/transcription/audio`, options);
                if (!res.ok && res.status !== 409) {
                    logErr('failed to send audio to transcribe', res.status);
                }
            });
        }
        transcriber.attach(client!.audioTrack);
    };

    // As long as the store (the webapp's): without a client, it only stops the transcriber.
    store.subscribe(syncTranscriber);

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
            client.off('initaudio', syncTranscriber);
            client.off('devicechange', syncTranscriber);
            client.off('connect', syncTranscriber);
        }
        transcriber?.stop();
        transcriber = null;
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
        client.on('initaudio', syncTranscriber);
        client.on('devicechange', syncTranscriber);
        client.on('connect', syncTranscriber);
        emit('change');
    };

    const clientClosed = (err?: Error) => {
        detach();
        if (err) {
            emit('error', {kind: 'call-error', error: err});
        }
        emit('change');
    };

    const hostAction = (action: (callID: string, sessionID: string) => Promise<unknown>) => (sessionId: string, channelId?: string) => {
        const channelID = channelId || channelIDForCurrentCall(store.getState());
        if (channelID) {
            action(channelID, sessionId).catch((err) => logErr(err));
        }
    };

    // The moderation requests reject with an Error carrying the HTTP status.
    const moderationRequest = async (channelId: string, action: string, body: object) => {
        try {
            await RestClient.fetch(`${getPluginPath()}/calls/${channelId}/host/${action}`, {method: 'post', body: JSON.stringify(body)});
        } catch (err) {
            const status = (err as {status_code?: number})?.status_code || 0;
            throw Object.assign(new Error((err as Error)?.message || String(err)), {status});
        }
    };

    const transcriptionAction = async (action: 'start' | 'stop') => {
        const channelID = channelIDForCurrentCall(store.getState());
        if (channelID) {
            await RestClient.fetch(`${getPluginPath()}/calls/${channelID}/transcription/${action}`, {method: 'post'});
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
            } else if (client.modDeafened || isModDeafened(store.getState(), client.channelID, getCurrentUserId(store.getState()))) {
                emit('error', {kind: 'mod-deafened'});
            } else if (client.modMuted || isModMuted(store.getState(), client.channelID, getCurrentUserId(store.getState()))) {
                emit('error', {kind: 'mod-muted'});
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

        startScreenShare: (quality, surface, source) => {
            const state = store.getState();
            if (!client || !allowScreenSharing(state) || getScreenSharingSessionId(state, client.channelID)) {
                return;
            }

            // The desktop app needs the user to pick a screen or window first: the caller's picker, or Calls' own.
            let sourceID = '';
            let withAudio = shareAudioWithScreen();
            if (window.desktop && source?.desktopSourceId) {
                sourceID = source.desktopSourceId;
                withAudio = withAudio && Boolean(source.withAudio);
            } else if (window.desktop && compareSemVer(window.desktop.version, '5.1.0') >= 0) {
                store.dispatch(showScreenSourceModal());
                return;
            }

            // This must run synchronously from the click: the browser only lets a page ask to
            // capture the screen right after a user action.
            client.shareScreen(sourceID, withAudio, quality, surface).then((stream) => {
                if (!stream) {
                    emit('error', {kind: 'screen-share-failed'});
                    return;
                }

                // Sharing can be stopped from the browser's own controls.
                stream.getVideoTracks()[0]?.addEventListener('ended', onChange, {once: true});
            }).catch((err) => logErr(err));
        },

        canShareSystemAudio: () => {
            const platform = getPlatformInfo();
            return Boolean(window.desktop) && shareAudioWithScreen() && (platform === 'Linux' || platform === 'Windows');
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

        sendReaction: (emojiName, unified) => {
            client?.sendUserReaction({name: emojiName, unified});
        },

        setParticipantVolume: (sessionId, volume) => setSessionVolume(sessionId, volume),
        getParticipantVolume: (sessionId) => getSessionVolume(sessionId),

        getBackgroundBlur: () => {
            const data = getBgBlurData();
            return {on: data.blurBackground, intensity: data.blurIntensity || 50};
        },
        setBackgroundBlur: async ({on, intensity}) => {
            if (client) {
                await client.setBackgroundBlur(on, intensity);
            } else {
                setBgBlurData({blurBackground: on, blurIntensity: intensity});
            }
            emit('change');
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

        moderation: {
            setModMuted: (channelId, userId, muted) => moderationRequest(channelId, 'mod-mute', {user_id: userId, muted}),
            askToUnmute: (channelId, sessionId) => moderationRequest(channelId, 'ask-unmute', {session_id: sessionId}),
            setModDeafened: (channelId, userId, deafened) => moderationRequest(channelId, 'mod-deafen', {user_id: userId, deafened}),
            move: (channelId, sessionId, toChannelId) => moderationRequest(channelId, 'move', {session_id: sessionId, channel_id: toChannelId}),
            listModMutes: async (channelId) => {
                let list: LastingMuteData[];
                try {
                    list = await RestClient.fetch<LastingMuteData[]>(`${getPluginPath()}/calls/${channelId}/host/mod-mutes`, {method: 'get'});
                } catch (err) {
                    const status = (err as {status_code?: number})?.status_code || 0;
                    throw Object.assign(new Error((err as Error)?.message || String(err)), {status});
                }
                return (list || []).map(lastingMute);
            },
            participantPermissions: async (channelId, sessionId) => {
                let data: ParticipantPermissionsData;
                try {
                    data = await RestClient.fetch<ParticipantPermissionsData>(`${getPluginPath()}/calls/${channelId}/host/permissions?session_id=${encodeURIComponent(sessionId)}`, {method: 'get'});
                } catch (err) {
                    const status = (err as {status_code?: number})?.status_code || 0;
                    throw Object.assign(new Error((err as Error)?.message || String(err)), {status});
                }
                return participantPermissions({...data, session_id: data?.session_id || sessionId});
            },
        },

        startTranscription: () => transcriptionAction('start'),
        stopTranscription: () => transcriptionAction('stop'),

        dismissIncomingCall: (channelId, callId) => {
            store.dispatch(dismissIncomingCallNotification(channelId, callId));
        },

        stopRinging: (callId) => {
            store.dispatch(stopRingingForCall(callId));
        },

        openSettings: () => {
            store.dispatch(openCallsUserSettings());
        },
    };

    const unmuteRequested = (request: CallsUnmuteRequest) => emit('unmute-request', request);

    // A move is for the window that has the call (or, with the desktop app, the main window): it switches to the
    // other channel's call, and the UI follows. The server disconnects the session from the first call otherwise.
    const moveRequested = async (request: CallsMoveRequest) => {
        const local = getLocalCall(store.getState());
        if (!local || local.channelId !== request.channelId || local.sessionId !== request.sessionId || local.channelId === request.toChannelId) {
            return;
        }
        emit('move', request);
        try {
            await joinCall(request.toChannelId, {switchCall: true});
        } catch (err) {
            logErr('failed to join the call moved to', err);
        }
    };

    return {api, clientCreated, clientClosed, unmuteRequested, moveRequested};
}
