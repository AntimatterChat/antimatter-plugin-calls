// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useEffect, useState} from 'react';
import {useIntl} from 'react-intl';
import {getParticipantPermissions, hostLowerHand, hostMake, hostMute, hostScreenOff, ParticipantPermissionsData} from 'src/actions';
import {DropdownMenuItem, DropdownMenuSeparator} from 'src/components/dot_menu/dot_menu';
import MinusCircleOutlineIcon from 'src/components/icons/minus_circle_outline';
import MonitorAccount from 'src/components/icons/monitor_account';
import MutedIcon from 'src/components/icons/muted_icon';
import UnraisedHandIcon from 'src/components/icons/unraised_hand';
import UnshareScreenIcon from 'src/components/icons/unshare_screen';
import styled from 'styled-components';

type Props = {
    callID?: string;
    userID: string;
    sessionID: string;
    isMuted: boolean;
    isSharingScreen: boolean;
    isHandRaised: boolean;
    isHost: boolean;
    onRemove: () => void;
}

// useParticipantPermissions asks the server what the current user may do to a participant (Antimatter), as the menu
// opens: undefined while it answers, null when it couldn't tell (as with an older server), where the menu offers
// every control as it used to.
const useParticipantPermissions = (callID: string | undefined, sessionID: string) => {
    const [perms, setPerms] = useState<ParticipantPermissionsData | null | undefined>(undefined);
    useEffect(() => {
        if (!callID) {
            return undefined;
        }
        let current = true;
        setPerms(undefined);
        getParticipantPermissions(callID, sessionID).then(
            (res) => current && setPerms(res || null),
            () => current && setPerms(null),
        );
        return () => {
            current = false;
        };
    }, [callID, sessionID]);
    return perms;
};

// mayDo tells whether the menu offers a control: not while the server answers, and only when allowed once it did.
const mayDo = (perms: ParticipantPermissionsData | null | undefined, flag: keyof Omit<ParticipantPermissionsData, 'session_id'>) => {
    if (perms === undefined) {
        return false;
    }
    return perms === null || perms[flag] !== false;
};

export const HostControlsMenu = ({
    callID,
    userID,
    sessionID,
    isMuted,
    isSharingScreen,
    isHandRaised,
    isHost,
    onRemove,
}: Props) => {
    const {formatMessage} = useIntl();
    const perms = useParticipantPermissions(callID, sessionID);

    if (!callID) {
        return null;
    }

    // Antimatter: nobody mutes or removes someone at or above their rank; the menu hides what the server would refuse.
    const canMute = mayDo(perms, 'can_mute');
    const canRemove = mayDo(perms, 'can_kick');

    const muteUnmute = isMuted || !canMute ? null : (
        <DropdownMenuItem onClick={() => hostMute(callID, sessionID)}>
            <MutedIcon
                data-testid={'host-mute'}
                fill='var(--center-channel-color-56)'
                style={{width: '16px', height: '16px'}}
            />
            {formatMessage({defaultMessage: 'Mute participant'})}
        </DropdownMenuItem>
    );

    const showingAtLeastOne = Boolean(muteUnmute) || isSharingScreen || isHandRaised || !isHost;

    return (
        <>
            {muteUnmute}
            {isSharingScreen &&
                <DropdownMenuItem onClick={() => hostScreenOff(callID, sessionID)}>
                    <UnshareScreenIcon
                        fill='var(--center-channel-color-56)'
                        style={{width: '16px', height: '16px'}}
                    />
                    {formatMessage({defaultMessage: 'Stop screen share'})}
                </DropdownMenuItem>
            }
            {isHandRaised &&
                <DropdownMenuItem onClick={() => hostLowerHand(callID, sessionID)}>
                    <UnraisedHandIcon
                        fill='var(--center-channel-color-56)'
                        style={{width: '16px', height: '16px'}}
                    />
                    {formatMessage({defaultMessage: 'Lower hand'})}
                </DropdownMenuItem>
            }
            {!isHost &&
                <DropdownMenuItem onClick={() => hostMake(callID, userID)}>
                    <MonitorAccount
                        fill='var(--center-channel-color-56)'
                        style={{width: '16px', height: '16px'}}
                    />
                    {formatMessage({defaultMessage: 'Make host'})}
                </DropdownMenuItem>
            }
            {showingAtLeastOne && canRemove &&
                <DropdownMenuSeparator/>
            }
            {canRemove &&
                <DropdownMenuItem onClick={onRemove}>
                    <MinusCircleOutlineIcon
                        data-testid={'host-remove'}
                        fill='var(--dnd-indicator)'
                        style={{width: '16px', height: '16px'}}
                    />
                    <RedText>{formatMessage({defaultMessage: 'Remove from call'})}</RedText>
                </DropdownMenuItem>
            }
        </>
    );
};

const RedText = styled.span`
    color: var(--dnd-indicator);
`;
