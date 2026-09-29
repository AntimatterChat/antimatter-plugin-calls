// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {GlobalState} from '@mattermost/types/store';
import {getCurrentChannel} from 'mattermost-redux/selectors/entities/channels';
import {getCurrentUserId, getUser} from 'mattermost-redux/selectors/entities/users';
import React, {useState} from 'react';
import {OverlayTrigger, Tooltip} from 'react-bootstrap';
import {useIntl} from 'react-intl';
import {useSelector} from 'react-redux';
import CompassIcon from 'src/components/icons/compassIcon';
import {Header, Spinner} from 'src/components/shared';
import {
    callsShowButton,
    channelIDForCurrentCall,
    clientConnecting,
    currentChannelHasCall,
    isLimitRestricted,
    maxParticipants,
} from 'src/selectors';
import {getUserIdFromDM, isDMChannel} from 'src/utils';
import styled, {css} from 'styled-components';

const ChannelHeaderButton = () => {
    const channel = useSelector(getCurrentChannel);
    const currentUserID = useSelector(getCurrentUserId);
    const otherUserID = getUserIdFromDM(channel?.name || '', currentUserID);
    const otherUser = useSelector((state: GlobalState) => getUser(state, otherUserID));
    const isDeactivatedDM = isDMChannel(channel) && otherUser?.delete_at > 0;
    const show = useSelector((state: GlobalState) => callsShowButton(state, channel?.id || ''));
    const inCall = useSelector(channelIDForCurrentCall) === channel?.id;
    const hasCall = useSelector(currentChannelHasCall);
    const limitRestricted = useSelector(isLimitRestricted);
    const maxCallParticipants = useSelector(maxParticipants);
    const isChannelArchived = channel && channel.delete_at > 0;
    const isClientConnecting = useSelector(clientConnecting);

    const {formatMessage} = useIntl();

    const [joining, setJoining] = useState(false); // doesn't matter, will be set below
    const onClick = () => setJoining(hasCall);

    if (!show || !channel) {
        return null;
    }

    const restricted = limitRestricted || isChannelArchived || isDeactivatedDM;

    let callButtonText;
    if (hasCall) {
        callButtonText = formatMessage({defaultMessage: 'Join call'});
    } else {
        callButtonText = formatMessage({defaultMessage: 'Start call'});
    }

    if (isClientConnecting && joining) {
        callButtonText = formatMessage({defaultMessage: 'Joining call…'});
    } else if (isClientConnecting) {
        callButtonText = formatMessage({defaultMessage: 'Starting call…'});
    }

    const button = (
        <CallButton
            id='calls-join-button'
            className={'style--none call-button ' + (inCall || restricted ? 'disabled' : '')}
            disabled={isChannelArchived || isDeactivatedDM || isClientConnecting}
            $restricted={restricted}
            $isClientConnecting={isClientConnecting}
            onClick={onClick}
        >
            {isClientConnecting ? <Spinner $size={12}/> : <CompassIcon icon='phone'/>}
            <CallButtonText>
                {callButtonText}
            </CallButtonText>
        </CallButton>
    );

    if (isChannelArchived) {
        return (
            <OverlayTrigger
                placement='bottom'
                rootClose={true}
                overlay={
                    <Tooltip id='tooltip-limit-header'>
                        {formatMessage({defaultMessage: 'Calls are not available in archived channels.'})}
                    </Tooltip>
                }
            >
                {button}
            </OverlayTrigger>
        );
    }

    if (isDeactivatedDM) {
        return (
            <OverlayTrigger
                placement='bottom'
                rootClose={true}
                overlay={
                    <Tooltip id='tooltip-limit-header'>
                        {formatMessage({defaultMessage: 'Calls are not available in a DM with a deactivated user.'})}
                    </Tooltip>
                }
            >
                {button}
            </OverlayTrigger>
        );
    }

    if (limitRestricted && !inCall) {
        return (
            <OverlayTrigger
                placement='bottom'
                rootClose={true}
                overlay={
                    <Tooltip id='tooltip-limit-header'>
                        <Header>
                            {formatMessage({defaultMessage: 'This call is at its maximum capacity of {count, plural, =1 {# participant} other {# participants}}.'}, {count: maxCallParticipants})}
                        </Header>
                    </Tooltip>
                }
            >
                {button}
            </OverlayTrigger>
        );
    }

    return button;
};

const CallButton = styled.button<{ $restricted: boolean, $isClientConnecting: boolean }>`
    gap: 6px;

    // &&& is to override the call-button styles
    &&& {
        ${(props) => props.$restricted && css`
            color: rgba(var(--center-channel-color-rgb), 0.48);
            border: 1px solid rgba(var(--center-channel-color-rgb), 0.16);
            margin-right: 4px;
        `}
        cursor: pointer;
    }

    ${(props) => props.$isClientConnecting && css`
      &&&& {
        background: rgba(var(--button-bg-rgb), 0.12);
        color: var(--button-bg);
      }
    `}
`;

const CallButtonText = styled.span`
  &&&& {
    font-size: 12px;
    line-height: 16px;
    font-weight: 600;
  }
`;

export default ChannelHeaderButton;
