// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {setThreadFollow} from 'mattermost-redux/actions/threads';

import {logErr} from './log';
import {followThread} from './utils';

jest.mock('mattermost-redux/actions/threads', () => ({
    setThreadFollow: jest.fn((userID, teamID, threadID, state) => ({
        type: 'MOCK_SET_THREAD_FOLLOW',
        userID,
        teamID,
        threadID,
        state,
    })),
}));

jest.mock('./log', () => ({
    logDebug: jest.fn(),
    logErr: jest.fn(),
    logWarn: jest.fn(),
}));

const pluginId = 'com.mattermost.calls';
const channelID = 'channel-id';
const teamID = 'team-id';
const userID = 'user-id';
const threadID = 'thread-id';

// A minimal store: just enough of the shape followThread's selectors read (plugin calls state,
// and the current user id), with a real dispatch so the dispatched thunk/action is observable.
const makeStore = (calls: Record<string, {ID: string; channelID: string; ownerID: string; startAt: number; threadID: string}>) => {
    const state = {
        [`plugins-${pluginId}`]: {calls},
        entities: {users: {currentUserId: userID, profiles: {}}},
    };
    return {
        getState: jest.fn(() => state),
        dispatch: jest.fn((action) => action),
    };
};

describe('followThread', () => {
    it('does nothing without a team id', async () => {
        const store = makeStore({});
        await followThread(store as any, channelID, undefined);
        expect(store.dispatch).not.toHaveBeenCalled();
        expect(logErr).not.toHaveBeenCalled();
    });

    it('logs an error when the call is not registered in the store', async () => {
        const store = makeStore({});
        await followThread(store as any, channelID, teamID);
        expect(store.dispatch).not.toHaveBeenCalled();
        expect(logErr).toHaveBeenCalledWith('Unable to follow call\'s thread, not registered in store');
    });

    it('skips following, without logging, when the registered call has no thread (voice-channel calls)', async () => {
        const store = makeStore({
            [channelID]: {ID: 'call-id', channelID, ownerID: userID, startAt: 0, threadID: ''},
        });
        await followThread(store as any, channelID, teamID);
        expect(store.dispatch).not.toHaveBeenCalled();
        expect(logErr).not.toHaveBeenCalled();
    });

    it('follows the thread for a normal channel/DM call that has one', async () => {
        const store = makeStore({
            [channelID]: {ID: 'call-id', channelID, ownerID: userID, startAt: 0, threadID},
        });
        await followThread(store as any, channelID, teamID);
        expect(setThreadFollow).toHaveBeenCalledWith(userID, teamID, threadID, true);
        expect(store.dispatch).toHaveBeenCalledWith({
            type: 'MOCK_SET_THREAD_FOLLOW',
            userID,
            teamID,
            threadID,
            state: true,
        });
        expect(logErr).not.toHaveBeenCalled();
    });
});
