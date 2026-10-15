// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {GlobalState} from '@mattermost/types/store';

import {isSilencedCall} from './actions';

const stateWith = (prefs: Record<string, {value: string}>, dmRequest?: string) => ({
    entities: {
        preferences: {myPreferences: prefs},
        channels: {myMembers: {dm1: {notify_props: dmRequest ? {dm_request: dmRequest} : {}}}},
    },
} as unknown as GlobalState);

describe('isSilencedCall', () => {
    test('rings for an ordinary direct message', () => {
        expect(isSilencedCall(stateWith({}), 'dm1', 'caller')).toBe(false);
        expect(isSilencedCall(stateWith({}, 'accepted'), 'dm1', 'caller')).toBe(false);
    });

    test('stays silent for an ignored or blocked caller', () => {
        expect(isSilencedCall(stateWith({'user_block--caller': {value: 'ignore'}}), 'dm1', 'caller')).toBe(true);
        expect(isSilencedCall(stateWith({'user_block--caller': {value: 'block'}}), 'dm1', 'caller')).toBe(true);
        expect(isSilencedCall(stateWith({'user_block--someone': {value: 'block'}}), 'dm1', 'caller')).toBe(false);
    });

    test('stays silent in a message request', () => {
        expect(isSilencedCall(stateWith({}, 'pending'), 'dm1', 'caller')).toBe(true);
        expect(isSilencedCall(stateWith({}, 'dismissed'), 'dm1', 'caller')).toBe(true);
    });
});
