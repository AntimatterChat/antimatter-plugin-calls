// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import {Store} from 'src/types/antimatter-webapp';

import {RECEIVED_CHANNEL_STATE} from './action_types';
import RestClient from './rest_client';
import {channelState} from './selectors';
import {ChannelProps} from './types/types';
import {getPluginPath} from './utils';

type ChannelStateResponse = {
    channel_id: string;
    enabled: boolean;
    props?: ChannelProps;

    // The channel has no setting of its own and follows the server's default (test mode).
    server_default?: boolean;
};

// Antimatter: the Calls settings of the user's channels are loaded with the plugin, but a channel created or joined
// since has none here, though calls may be off in it (outside workplace communities, calls are off unless enabled
// in the channel). The fetcher asks the server, once per channel, when the user opens such a channel.
export function makeChannelStateFetcher(store: Store) {
    const asked = new Set<string>();
    return async (channelID: string) => {
        if (!channelID || asked.has(channelID) || channelState(store.getState(), channelID)) {
            return;
        }
        asked.add(channelID);
        try {
            const data = await RestClient.fetch<ChannelStateResponse>(`${getPluginPath()}/${channelID}`, {method: 'get'});

            // The server's default needs no state: the clients apply it (system admins can still call in test mode).
            // A state may also have arrived meanwhile, through a websocket event: it's newer.
            if (data.server_default || channelState(store.getState(), channelID)) {
                return;
            }
            store.dispatch({
                type: RECEIVED_CHANNEL_STATE,
                data: {id: channelID, enabled: data.enabled, props: data.props},
            });
        } catch {
            // Not allowed to read the channel, or the server is unreachable: the call button stays as it is.
            asked.delete(channelID);
        }
    };
}
