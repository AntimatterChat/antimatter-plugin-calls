// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {useEffect} from 'react';
import {CallAudio} from 'src/call_audio';
import {useRingback} from 'src/components/incoming_calls/hooks';
import {logErr} from 'src/log';

// HeadlessCall is mounted instead of the call widget when another UI draws the call (the Fusion
// web UI, through window.antimatterCalls). It does the widget's work that isn't drawing: it plays
// the call's audio and the ringback a DM caller hears while waiting for an answer.
export default function HeadlessCall() {
    useEffect(() => {
        if (!window.callsClient) {
            logErr('callsClient should be defined');
            return undefined;
        }

        const callAudio = new CallAudio(window.callsClient);
        return () => callAudio.destroy();
    }, []);

    useRingback();

    return null;
}
