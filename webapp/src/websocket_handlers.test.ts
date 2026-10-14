// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {CallHostChangedData, UserJoinedData, UserLeftData} from '@mattermost/calls-common/lib/types';
import {WebSocketMessage} from '@mattermost/client/websocket';
import {getCurrentUserId, getUser} from 'mattermost-redux/selectors/entities/users';
import {loadProfilesByIdsIfMissing, removeIncomingCallNotification, setDMCalleeAnsweredAt, userLeft} from 'src/actions';
import {Store} from 'src/types/antimatter-webapp';

import {CALL_HOST, HOST_CONTROL_NOTICE, MOD_MUTE_CHANGED, USER_JOINED} from './action_types';
import {
    calls,
    dmCalleeAnsweredAtForCurrentCall,
    idForCurrentCall,
    isCurrentUserOwnerOfCurrentCall,
    ringingEnabled,
    sessionsInCurrentCall,
    shouldPlayJoinUserSound,
} from './selectors';
import {
    getCallsClient,
    getCallsClientChannelID,
    getCallsClientSessionID,
    getCallsWindow,
    isDMChannel,
    notificationsStopRinging,
    playSound,
} from './utils';
import {handleAskUnmute, handleCallHostChanged, handleModMute, handleUserJoined, handleUserLeft} from './websocket_handlers';

jest.mock('mattermost-redux/selectors/entities/channels', () => ({
    getChannel: jest.fn(() => ({id: 'channel-id', type: 'D'})),
}));

jest.mock('mattermost-redux/selectors/entities/users', () => ({
    getCurrentUserId: jest.fn(),
    getUser: jest.fn(),
}));

jest.mock('src/actions', () => ({
    callEnd: jest.fn(),
    incomingCallOnChannel: jest.fn(),
    loadCallState: jest.fn(),
    loadProfilesByIdsIfMissing: jest.fn((ids) => ({type: 'loadProfilesByIdsIfMissing', ids})),
    removeIncomingCallNotification: jest.fn((callID) => ({type: 'removeIncomingCallNotification', callID})),
    setDMCalleeAnsweredAt: jest.fn((callID, answeredAt) => ({type: 'setDMCalleeAnsweredAt', callID, answeredAt})),
    userLeft: jest.fn(),
}));

jest.mock('./selectors', () => ({
    calls: jest.fn(() => ({})),
    channelIDForCurrentCall: jest.fn(),
    dmCalleeAnsweredAtForCurrentCall: jest.fn(),
    idForCurrentCall: jest.fn(),
    isCurrentUserOwnerOfCurrentCall: jest.fn(),
    profilesInCurrentCallMap: jest.fn(() => ({})),
    ringingEnabled: jest.fn(),
    sessionsInCurrentCall: jest.fn(() => []),
    shouldPlayJoinUserSound: jest.fn(),
}));

jest.mock('./utils', () => ({
    followThread: jest.fn(),
    getCallsClient: jest.fn(),
    getCallsClientChannelID: jest.fn(),
    getCallsClientSessionID: jest.fn(),
    getCallsWindow: jest.fn(),
    getUserDisplayName: jest.fn(),
    isDMChannel: jest.fn(),
    notificationsStopRinging: jest.fn(),
    playSound: jest.fn(),
}));

jest.mock('./log', () => ({
    logErr: jest.fn(),
    logInfo: jest.fn(),
}));

const channelID = 'channel-id';
const callID = 'call-id';
const currentUserID = 'my-user-id';
const otherUserID = 'other-user-id';

const mock = (fn: unknown) => fn as jest.Mock;

const userJoinedEvent = (userID: string, sessionID = 'session-id') => ({
    data: {user_id: userID, channelID, session_id: sessionID},
    broadcast: {channel_id: channelID},
} as unknown as WebSocketMessage<UserJoinedData>);

type Opts = {

    // Whether we are in the call the event is about.
    joined?: boolean;
    isDM?: boolean;
    iAmOwner?: boolean;
    alreadyAnswered?: number;
    currentCallID?: string;
    currentCallData?: { dmCalleeAnsweredAt?: number } | null;
};

const setup = ({
    joined = true,
    isDM = true,
    iAmOwner = true,
    alreadyAnswered = 0,
    currentCallID = callID,
    currentCallData = {},
}: Opts = {}) => {
    mock(getCurrentUserId).mockReturnValue(currentUserID);
    mock(getCallsClientChannelID).mockReturnValue(joined ? channelID : '');
    mock(getCallsClientSessionID).mockReturnValue('my-session-id');
    mock(isDMChannel).mockReturnValue(isDM);
    mock(idForCurrentCall).mockReturnValue(currentCallID);
    mock(isCurrentUserOwnerOfCurrentCall).mockReturnValue(iAmOwner);
    mock(dmCalleeAnsweredAtForCurrentCall).mockReturnValue(alreadyAnswered);
    mock(ringingEnabled).mockReturnValue(false);
    mock(shouldPlayJoinUserSound).mockReturnValue(false);
    mock(calls).mockReturnValue({});

    const callsWindow = {currentCallData};
    mock(getCallsWindow).mockReturnValue(callsWindow);

    const store = {
        dispatch: jest.fn(),
        getState: jest.fn(() => ({})),
    } as unknown as Store;

    return {store, callsWindow};
};

const dispatched = (store: Store, type: string) =>
    mock(store.dispatch).mock.calls.map((c) => c[0]).filter((a) => a && a.type === type);

describe('handleUserJoined', () => {
    beforeEach(() => {
        jest.useFakeTimers().setSystemTime(new Date('2026-01-01T00:00:00Z'));
        window.callsClient = undefined;
    });

    afterEach(() => {
        jest.useRealTimers();
    });

    test('should record when the callee answered a DM call', () => {
        const {store} = setup();

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).toHaveBeenCalledWith(callID, Date.now());
        expect(dispatched(store, 'setDMCalleeAnsweredAt')).toHaveLength(1);
    });

    test('should copy the answer time onto the window so the expanded view can pick it up', () => {
        const {store, callsWindow} = setup();

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(callsWindow.currentCallData).toEqual({dmCalleeAnsweredAt: Date.now()});
    });

    test('should not fail when there is no current call data on the window', () => {
        const {store} = setup({currentCallData: null});

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).toHaveBeenCalled();
    });

    test('should not record an answer when we are not in a call', () => {
        const {store} = setup({currentCallID: ''});

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).not.toHaveBeenCalled();
    });

    test('should not record an answer for a call in another channel', () => {
        const {store} = setup({joined: false});

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).not.toHaveBeenCalled();
    });

    test('should not record an answer when we are the one joining', () => {
        const {store} = setup();

        handleUserJoined(store, userJoinedEvent(currentUserID));

        expect(setDMCalleeAnsweredAt).not.toHaveBeenCalled();
    });

    test('should not record an answer outside a DM channel', () => {
        const {store} = setup({isDM: false});

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).not.toHaveBeenCalled();
    });

    test('should not record an answer for the callee, who is not the caller', () => {
        const {store} = setup({iAmOwner: false});

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).not.toHaveBeenCalled();
    });

    test('should not record an answer twice', () => {
        const {store} = setup({alreadyAnswered: 1234});

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(setDMCalleeAnsweredAt).not.toHaveBeenCalled();
    });

    test('should play the self join sound when our own session joins', () => {
        const {store} = setup();
        window.callsClient = {channelID} as never;

        handleUserJoined(store, userJoinedEvent(currentUserID, 'my-session-id'));

        expect(playSound).toHaveBeenCalledWith('join_self');
    });

    test('should play the other user join sound when someone else joins', () => {
        const {store} = setup();
        window.callsClient = {channelID} as never;
        mock(shouldPlayJoinUserSound).mockReturnValue(true);

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(playSound).toHaveBeenCalledWith('join_user');
    });

    test('should dismiss the incoming call notification once we join', () => {
        const {store} = setup();
        mock(ringingEnabled).mockReturnValue(true);
        mock(calls).mockReturnValue({[channelID]: {ID: callID}});

        handleUserJoined(store, userJoinedEvent(currentUserID));

        expect(removeIncomingCallNotification).toHaveBeenCalledWith(callID);
        expect(notificationsStopRinging).toHaveBeenCalled();
    });

    test('should load the profile of the user that joined', () => {
        const {store} = setup();

        handleUserJoined(store, userJoinedEvent(otherUserID));

        expect(loadProfilesByIdsIfMissing).toHaveBeenCalledWith([otherUserID]);
        expect(dispatched(store, USER_JOINED)).toHaveLength(1);
    });
});

describe('handleCallHostChanged', () => {
    const hostChangedEvent = (hostID: string) => ({
        data: {hostID, channelID, call_id: callID},
        broadcast: {channel_id: channelID},
    } as unknown as WebSocketMessage<CallHostChangedData>);

    beforeEach(() => {
        mock(getUser).mockReturnValue({id: 'host-id'});
    });

    test('should not announce that we are the host when placing a DM call', () => {
        const {store} = setup();

        handleCallHostChanged(store, hostChangedEvent(currentUserID));

        expect(dispatched(store, HOST_CONTROL_NOTICE)).toHaveLength(0);
    });

    test('should announce that we are the host of a DM call we already know about', () => {
        const {store} = setup();
        mock(calls).mockReturnValue({[channelID]: {ID: callID}});

        handleCallHostChanged(store, hostChangedEvent(currentUserID));

        expect(dispatched(store, HOST_CONTROL_NOTICE)).toHaveLength(1);
    });

    test('should announce that we are the host outside a DM channel', () => {
        const {store} = setup({isDM: false});

        handleCallHostChanged(store, hostChangedEvent(currentUserID));

        expect(dispatched(store, HOST_CONTROL_NOTICE)).toHaveLength(1);
    });

    test('should announce when someone else becomes the host', () => {
        const {store} = setup();

        handleCallHostChanged(store, hostChangedEvent(otherUserID));

        expect(dispatched(store, HOST_CONTROL_NOTICE)).toHaveLength(1);
    });

    test('should record the new host regardless of whether it is announced', () => {
        const {store} = setup();

        handleCallHostChanged(store, hostChangedEvent(currentUserID));

        expect(dispatched(store, CALL_HOST)).toHaveLength(1);
    });
});

describe('handleUserLeft', () => {
    const userLeftEvent = (userID: string, sessionID = 'session-id') => ({
        data: {user_id: userID, channelID, session_id: sessionID},
        broadcast: {channel_id: channelID},
    } as unknown as WebSocketMessage<UserLeftData>);

    beforeEach(() => {
        mock(playSound).mockClear();
        window.callsClient = {channelID} as unknown as typeof window.callsClient;
    });

    afterEach(() => {
        window.callsClient = undefined;
    });

    test('plays the leave sound when someone else leaves our call', () => {
        const {store} = setup();
        mock(shouldPlayJoinUserSound).mockReturnValue(true);

        handleUserLeft(store, userLeftEvent(otherUserID));

        expect(playSound).toHaveBeenCalledWith('leave_user');
        expect(userLeft).toHaveBeenCalledWith(channelID, otherUserID, 'session-id');
    });

    test('stays quiet in large calls, as for joins', () => {
        const {store} = setup();
        mock(shouldPlayJoinUserSound).mockReturnValue(false);

        handleUserLeft(store, userLeftEvent(otherUserID));

        expect(playSound).not.toHaveBeenCalled();
    });

    test('stays quiet for our own sessions and for other calls', () => {
        const {store} = setup();
        mock(shouldPlayJoinUserSound).mockReturnValue(true);

        handleUserLeft(store, userLeftEvent(currentUserID, 'other-device'));
        handleUserLeft(store, userLeftEvent(otherUserID, 'my-session-id'));
        window.callsClient = {channelID: 'another-channel'} as unknown as typeof window.callsClient;
        handleUserLeft(store, userLeftEvent(otherUserID));

        expect(playSound).not.toHaveBeenCalled();
    });
});

describe('moderator mutes', () => {
    const makeStore = () => ({dispatch: jest.fn(), getState: jest.fn(() => ({}))}) as unknown as Store;
    const fakeClient = (extra = {}) => ({channelID, modMuted: false, mute: jest.fn(), getSessionID: () => 'my-session', ...extra});

    beforeEach(() => {
        jest.clearAllMocks();
        (getCurrentUserId as jest.Mock).mockReturnValue(currentUserID);
    });

    test('handleModMute records the change and mutes the user in their call', () => {
        const store = makeStore();
        const client = fakeClient();
        (getCallsClient as jest.Mock).mockReturnValue(client);

        handleModMute(store, {data: {channel_id: channelID, user_id: currentUserID, muted: true, by: otherUserID}} as WebSocketMessage<any>);
        expect(store.dispatch).toHaveBeenCalledWith({type: MOD_MUTE_CHANGED, data: {channelID, userID: currentUserID, muted: true, by: otherUserID}});
        expect(client.modMuted).toBe(true);
        expect(client.mute).toHaveBeenCalled();

        // Lifting it leaves them muted.
        handleModMute(store, {data: {channel_id: channelID, user_id: currentUserID, muted: false, by: otherUserID}} as WebSocketMessage<any>);
        expect(client.modMuted).toBe(false);
        expect(client.mute).toHaveBeenCalledTimes(1);
    });

    test('handleModMute leaves the client alone for others or other calls', () => {
        const store = makeStore();
        const client = fakeClient();
        (getCallsClient as jest.Mock).mockReturnValue(client);

        handleModMute(store, {data: {channel_id: channelID, user_id: otherUserID, muted: true, by: currentUserID}} as WebSocketMessage<any>);
        handleModMute(store, {data: {channel_id: 'other', user_id: currentUserID, muted: true, by: otherUserID}} as WebSocketMessage<any>);
        expect(store.dispatch).toHaveBeenCalledTimes(2);
        expect(client.modMuted).toBe(false);
        expect(client.mute).not.toHaveBeenCalled();
    });

    test('handleAskUnmute passes on requests for this window\'s muted session', () => {
        const store = makeStore();
        const onRequest = jest.fn();
        const ev = {data: {channel_id: channelID, session_id: 'my-session', by: otherUserID}} as WebSocketMessage<any>;

        (getCallsClient as jest.Mock).mockReturnValue(fakeClient());
        handleAskUnmute(store, ev, onRequest);
        expect(onRequest).toHaveBeenCalledWith({channelId: channelID, sessionId: 'my-session', byUserId: otherUserID});

        // Not when unmuted already, moderator-muted, or for another session.
        onRequest.mockClear();
        (sessionsInCurrentCall as unknown as jest.Mock).mockReturnValueOnce([{session_id: 'my-session', unmuted: true}]);
        handleAskUnmute(store, ev, onRequest);
        (getCallsClient as jest.Mock).mockReturnValue(fakeClient({modMuted: true}));
        handleAskUnmute(store, ev, onRequest);
        (getCallsClient as jest.Mock).mockReturnValue(fakeClient({getSessionID: () => 'other-session'}));
        handleAskUnmute(store, ev, onRequest);
        expect(onRequest).not.toHaveBeenCalled();
    });
});
