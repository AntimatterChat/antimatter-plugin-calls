// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {render} from '@testing-library/react';
import React from 'react';
import {Provider} from 'react-redux';
import {mockStore} from 'src/testUtils';
import {IncomingCallNotification} from 'src/types/types';

import {IncomingCallContainer} from './call_container';

const mockOnACall = jest.fn();
const mockNoTeams: unknown[] = [];

jest.mock('src/components/incoming_calls/call_incoming', () => ({
    CallIncoming: ({call}: {call: IncomingCallNotification}) => <div data-testid={`incoming-${call.callID}`}/>,
}));

jest.mock('src/components/incoming_calls/call_incoming_condensed', () => ({
    CallIncomingCondensed: ({call}: {call: IncomingCallNotification}) => <div data-testid={`condensed-${call.callID}`}/>,
}));

jest.mock('src/components/incoming_calls/hooks', () => ({
    useOnACallWithoutGlobalWidget: () => mockOnACall(),
}));

jest.mock('mattermost-redux/selectors/entities/teams', () => ({
    ...jest.requireActual('mattermost-redux/selectors/entities/teams'),
    getMyTeams: () => mockNoTeams,
}));

const incomingCall = {callID: 'call-id', channelID: 'channel-id', callerID: 'caller-id', startAt: 1, type: 1} as IncomingCallNotification;

const renderContainer = () => render(
    <Provider
        store={mockStore({
            'plugins-com.mattermost.calls': {
                callsConfig: {EnableRinging: true},
                incomingCalls: [incomingCall],
            },
        })}
    >
        <IncomingCallContainer/>
    </Provider>,
);

describe('IncomingCallContainer', () => {
    afterEach(() => {
        delete window.antimatterWebUI;
    });

    test('shows incoming calls when not in a call', () => {
        mockOnACall.mockReturnValue(false);
        expect(renderContainer().queryByTestId('incoming-call-id')).not.toBeNull();
    });

    test('leaves incoming calls to the widget while in a call', () => {
        mockOnACall.mockReturnValue(true);
        expect(renderContainer().queryByTestId('incoming-call-id')).toBeNull();
    });

    test('shows incoming calls while in a call under Fusion, which has no widget', () => {
        window.antimatterWebUI = 'fusion';
        mockOnACall.mockReturnValue(true);
        expect(renderContainer().queryByTestId('incoming-call-id')).not.toBeNull();
    });
});
