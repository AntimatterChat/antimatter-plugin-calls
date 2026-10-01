// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {render} from '@testing-library/react';
import React from 'react';
import {CallAudio} from 'src/call_audio';
import type CallsClient from 'src/client';
import {useRingback} from 'src/components/incoming_calls/hooks';

import HeadlessCall from './headless_call';

jest.mock('src/call_audio', () => ({
    CallAudio: jest.fn().mockImplementation(() => ({destroy: jest.fn()})),
}));

jest.mock('src/components/incoming_calls/hooks', () => ({
    useRingback: jest.fn(),
}));

describe('HeadlessCall', () => {
    afterEach(() => {
        delete window.callsClient;
    });

    test('plays the call audio and the ringback, without drawing anything', () => {
        const client = {} as CallsClient;
        window.callsClient = client;

        const {container, unmount} = render(<HeadlessCall/>);

        expect(container.innerHTML).toBe('');
        expect(CallAudio).toHaveBeenCalledWith(client);
        expect(useRingback).toHaveBeenCalled();

        const callAudio = (CallAudio as jest.Mock).mock.results[0].value;
        expect(callAudio.destroy).not.toHaveBeenCalled();
        unmount();
        expect(callAudio.destroy).toHaveBeenCalled();
    });
});
