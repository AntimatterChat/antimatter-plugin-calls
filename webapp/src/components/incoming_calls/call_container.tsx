// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {getMyTeams} from 'mattermost-redux/selectors/entities/teams';
import React from 'react';
import {useSelector} from 'react-redux';
import {CallIncoming} from 'src/components/incoming_calls/call_incoming';
import {CallIncomingCondensed} from 'src/components/incoming_calls/call_incoming_condensed';
import {useOnACallWithoutGlobalWidget} from 'src/components/incoming_calls/hooks';
import {ringingEnabled, sortedIncomingCalls} from 'src/selectors';
import {shouldRenderCallsIncoming} from 'src/utils';
import {isFusionUI} from 'src/web_ui';
import styled from 'styled-components';

export const IncomingCallContainer = () => {
    const enabled = useSelector(ringingEnabled);
    const calls = [...useSelector(sortedIncomingCalls)];
    const myTeams = useSelector(getMyTeams);
    const onACallWithoutGlobalWidget = useOnACallWithoutGlobalWidget();

    if (!enabled || !shouldRenderCallsIncoming() || calls.length === 0 || (onACallWithoutGlobalWidget && !isFusionUI())) {
        // don't show incoming calls if we're on a call without the global widget because
        // we'll see the notification above the widget (there's no widget under Fusion)
        return null;
    }

    const wider = myTeams?.length > 1;
    const firstCall = calls.splice(-1)[0];

    return (
        <Container
            $wider={wider}
            $fusion={isFusionUI()}
        >
            {calls.map((c) => (
                <CallIncomingCondensed
                    key={c.callID}
                    call={c}
                />
            ))}
            <CallIncoming call={firstCall}/>
        </Container>
    );
};

// Classic: bottom left, over the team sidebar. Fusion: top right, below the header and clear of the app rail, so it
// covers neither the server rail and sidebar nor the message box.
const Container = styled.div<{ $wider: boolean, $fusion: boolean }>`
    position: absolute;
    display: flex;
    flex-direction: column;
    gap: 5px;
    z-index: 102;
    width: ${(props) => (props.$wider ? '306px' : '248px')};
    ${(props) => (props.$fusion ? `
        top: 64px;
        right: 64px;
    ` : `
        bottom: 10px;
        left: 12px;
    `)}
`;
