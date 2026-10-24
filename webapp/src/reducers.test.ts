// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {AnyAction} from 'redux';
import {
    ADD_INCOMING_CALL,
    CALL_END,
    CALL_STATE,
    DM_CALLEE_ANSWERED_AT,
    LOCAL_CALL_STATE,
    MOD_DEAFEN_CHANGED,
    MOD_MUTE_CHANGED,
    MOD_MUTED_STATE,
    REMOVE_INCOMING_CALL,
    STAGE_SPEAKERS_CHANGED,
    UNINIT,
} from 'src/action_types';

import reducer from './reducers';

type State = ReturnType<typeof reducer>;

const channelID = 'dm-channel-id';
const callID = 'call-id';
const ownerID = 'user-id';
const startAt = 1700000000000;
const answeredAt = startAt + 20_000;

const apply = (...actions: AnyAction[]): State =>
    actions.reduce((state: State | undefined, action) => reducer(state, action), undefined) as State;

// Mirrors what loadCallState dispatches, which is what the client gets back after asking for the
// call state on every websocket reconnect.
const callState = (ID: string) => ({
    type: CALL_STATE,
    data: {ID, channelID, startAt, ownerID, threadID: ''},
});

const callAnswered = (ID: string, at: number) => ({
    type: DM_CALLEE_ANSWERED_AT,
    data: {callID: ID, answeredAt: at},
});

// callEnd() is the only dispatcher of CALL_END and always includes the call ID.
const callEnd = (ID: string) => ({
    type: CALL_END,
    data: {channelID, callID: ID},
});

describe('dmCalleeAnsweredAt', () => {
    it('should record when the call was answered', () => {
        const state = apply(callState(callID), callAnswered(callID, answeredAt));

        expect(state.dmCalleeAnsweredAt).toEqual({[callID]: answeredAt});
    });

    // The regression this keying protects against: a websocket reconnect re-requests the call
    // state, and the resulting CALL_STATE must not restart the caller's duration timer.
    it('should survive a CALL_STATE resync of an already answered call', () => {
        const state = apply(callState(callID), callAnswered(callID, answeredAt), callState(callID));

        expect(state.dmCalleeAnsweredAt).toEqual({[callID]: answeredAt});
    });

    it('should not let a new call in the same channel inherit the previous answer time', () => {
        const nextCallID = 'next-call-id';
        const state = apply(
            callState(callID),
            callAnswered(callID, answeredAt),
            callEnd(callID),
            callState(nextCallID),
        );

        expect(state.dmCalleeAnsweredAt[nextCallID]).toBeUndefined();
        expect(state.dmCalleeAnsweredAt).toEqual({});
    });

    it('should keep the answer time of an ongoing call when another call ends', () => {
        const state = apply(callState(callID), callAnswered(callID, answeredAt), callEnd('other-call-id'));

        expect(state.dmCalleeAnsweredAt).toEqual({[callID]: answeredAt});
    });

    it('should drop everything on UNINIT', () => {
        const state = apply(callState(callID), callAnswered(callID, answeredAt), {type: UNINIT});

        expect(state.dmCalleeAnsweredAt).toEqual({});
    });
});

const incomingCall = (cID: string, cAllID: string) => ({
    type: ADD_INCOMING_CALL,
    data: {callID: cAllID, channelID: cID, callerID: 'caller-id', startAt, type: 'D'},
});

const removeIncomingCall = (cAllID: string) => ({
    type: REMOVE_INCOMING_CALL,
    data: {callID: cAllID},
});

describe('incomingCalls', () => {
    it('should add and remove incoming call notifications', () => {
        const state = apply(incomingCall(channelID, callID));

        expect(state.incomingCalls).toHaveLength(1);
        expect(state.incomingCalls[0].callID).toBe(callID);

        const state2 = apply(incomingCall(channelID, callID), removeIncomingCall(callID));

        expect(state2.incomingCalls).toHaveLength(0);
    });

    it('should clear all incoming call notifications on UNINIT', () => {
        const state = apply(
            incomingCall(channelID, callID),
            incomingCall('other-channel-id', 'other-call-id'),
            {type: UNINIT},
        );

        expect(state.incomingCalls).toHaveLength(0);
    });
});

describe('localCall', () => {
    const connecting = {channelID, sessionID: '', state: 'connecting'};
    const connected = {channelID, sessionID: 'session-id', state: 'connected'};

    it('should start without a call', () => {
        expect(apply({type: 'init'}).localCall).toBeNull();
    });

    it('should follow the call from connecting to closed', () => {
        expect(apply({type: LOCAL_CALL_STATE, data: connecting}).localCall).toEqual(connecting);
        expect(apply(
            {type: LOCAL_CALL_STATE, data: connecting},
            {type: LOCAL_CALL_STATE, data: connected},
        ).localCall).toEqual(connected);
        expect(apply(
            {type: LOCAL_CALL_STATE, data: connected},
            {type: LOCAL_CALL_STATE, data: null},
        ).localCall).toBeNull();
    });

    it('should be cleared on UNINIT', () => {
        expect(apply({type: LOCAL_CALL_STATE, data: connected}, {type: UNINIT}).localCall).toBeNull();
    });
});

describe('modMuted', () => {
    const changed = (userID: string, muted: boolean, by = 'admin') => ({type: MOD_MUTE_CHANGED, data: {channelID, userID, muted, by}});

    it('should load the moderator mutes of the call state', () => {
        const state = apply({type: MOD_MUTED_STATE, data: {channelID, modMuted: {u1: 'admin'}}});
        expect(state.modMuted).toEqual({[channelID]: {u1: 'admin'}});
        expect(apply(
            {type: MOD_MUTED_STATE, data: {channelID, modMuted: {u1: 'admin'}}},
            {type: MOD_MUTED_STATE, data: {channelID}},
        ).modMuted).toEqual({});
    });

    it('should follow the mod_mute events', () => {
        expect(apply(changed('u1', true)).modMuted).toEqual({[channelID]: {u1: 'admin'}});
        expect(apply(changed('u1', true), changed('u2', true, 'host')).modMuted).toEqual({[channelID]: {u1: 'admin', u2: 'host'}});
        expect(apply(changed('u1', true), changed('u1', false)).modMuted).toEqual({});
    });

    it('should keep the state when lifting someone not muted', () => {
        const before = apply(changed('u1', true));
        expect(reducer(before, changed('u2', false)).modMuted).toBe(before.modMuted);
    });

    it('should drop the channel\'s mutes as its call ends', () => {
        expect(apply(changed('u1', true), callEnd(callID)).modMuted).toEqual({});
    });
});

describe('modDeafened', () => {
    const deafened = (userID: string, on: boolean, by = 'admin') => ({type: MOD_DEAFEN_CHANGED, data: {channelID, userID, deafened: on, by}});

    it('should load the moderator deafens of the call state, apart from the mutes', () => {
        const state = apply({type: MOD_MUTED_STATE, data: {channelID, modMuted: {u1: 'host'}, modDeafened: {u2: 'admin'}}});
        expect(state.modDeafened).toEqual({[channelID]: {u2: 'admin'}});
        expect(state.modMuted).toEqual({[channelID]: {u1: 'host'}});
        expect(apply(
            {type: MOD_MUTED_STATE, data: {channelID, modDeafened: {u2: 'admin'}}},
            {type: MOD_MUTED_STATE, data: {channelID}},
        ).modDeafened).toEqual({});
    });

    it('should follow the mod_deafen events only', () => {
        expect(apply(deafened('u1', true)).modDeafened).toEqual({[channelID]: {u1: 'admin'}});
        expect(apply(deafened('u1', true)).modMuted).toEqual({});
        expect(apply(deafened('u1', true), deafened('u1', false)).modDeafened).toEqual({});
        expect(apply({type: MOD_MUTE_CHANGED, data: {channelID, userID: 'u1', muted: true, by: 'admin'}}).modDeafened).toEqual({});
    });

    it('should drop the channel\'s deafens as its call ends', () => {
        expect(apply(deafened('u1', true), callEnd(callID)).modDeafened).toEqual({});
    });
});

describe('stages', () => {
    const stage = (speakers: string[] | null, on = true) => ({type: STAGE_SPEAKERS_CHANGED, data: {channelID, stage: on, speakers}});

    it('should keep the speakers of a stage\'s call', () => {
        expect(apply(stage(['u1', 'u2'])).stages).toEqual({[channelID]: ['u1', 'u2']});
        expect(apply(stage(null)).stages).toEqual({[channelID]: []});
        expect(apply(stage(['u1']), stage(['u1', 'u3'])).stages).toEqual({[channelID]: ['u1', 'u3']});
    });

    it('should drop the channel as its call stops being a stage\'s, or ends', () => {
        expect(apply(stage(['u1']), stage(null, false)).stages).toEqual({});
        expect(apply(stage(['u1']), callEnd(callID)).stages).toEqual({});
        const state = apply(stage(null, false));
        expect(state.stages).toEqual({});
    });
});
