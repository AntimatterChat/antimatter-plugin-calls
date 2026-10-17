// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import RestClient from 'src/rest_client';
import {Store} from 'src/types/antimatter-webapp';

import {RECEIVED_CHANNEL_STATE} from './action_types';
import {makeChannelStateFetcher} from './channel_state';

jest.mock('src/rest_client', () => ({
    __esModule: true,
    default: {fetch: jest.fn()},
}));

const fetch = RestClient.fetch as jest.Mock;

const makeStore = (channels: Record<string, unknown> = {}) => {
    const state = {'plugins-com.mattermost.calls': {channels}};
    return {getState: () => state, dispatch: jest.fn()} as unknown as Store & {dispatch: jest.Mock};
};

describe('makeChannelStateFetcher', () => {
    beforeEach(() => fetch.mockReset());

    test('stores calls turned off by the community\'s kind, once', async () => {
        const store = makeStore();
        fetch.mockResolvedValue({channel_id: 'ch', enabled: false});

        const fetchState = makeChannelStateFetcher(store);
        await fetchState('ch');
        await fetchState('ch');

        expect(fetch).toHaveBeenCalledTimes(1);
        expect(fetch.mock.calls[0][0]).toMatch(/\/ch$/);
        expect(store.dispatch).toHaveBeenCalledWith({type: RECEIVED_CHANNEL_STATE, data: {id: 'ch', enabled: false, props: undefined}});
    });

    test('keeps no state for a channel that follows the server\'s default, so system admins can call in test mode', async () => {
        const store = makeStore();
        fetch.mockResolvedValue({channel_id: 'ch', enabled: false, server_default: true});

        await makeChannelStateFetcher(store)('ch');
        expect(store.dispatch).not.toHaveBeenCalled();
    });

    test('doesn\'t ask for a channel whose state is known', async () => {
        const store = makeStore({ch: {id: 'ch', enabled: true}});

        await makeChannelStateFetcher(store)('ch');
        expect(fetch).not.toHaveBeenCalled();
    });

    test('asks again after a failure', async () => {
        const store = makeStore();
        fetch.mockRejectedValueOnce(new Error('forbidden'));
        fetch.mockResolvedValueOnce({channel_id: 'ch', enabled: true, props: {enable_video: false}});

        const fetchState = makeChannelStateFetcher(store);
        await fetchState('ch');
        expect(store.dispatch).not.toHaveBeenCalled();

        await fetchState('ch');
        expect(store.dispatch).toHaveBeenCalledWith({type: RECEIVED_CHANNEL_STATE, data: {id: 'ch', enabled: true, props: {enable_video: false}}});
    });
});
