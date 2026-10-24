// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {act, render, screen} from '@testing-library/react';
import React from 'react';
import {createIntl, RawIntlProvider} from 'react-intl';
import {getParticipantPermissions} from 'src/actions';

import {HostControlsMenu} from './host_controls_menu';

jest.mock('src/actions', () => ({
    getParticipantPermissions: jest.fn(),
    hostLowerHand: jest.fn(),
    hostMake: jest.fn(),
    hostMute: jest.fn(),
    hostScreenOff: jest.fn(),
}));

// The dot menu pulls in @floating-ui, whose UMD build doesn't load under jest.
jest.mock('src/components/dot_menu/dot_menu', () => ({
    __esModule: true,
    default: 'div',
    DropdownMenuItem: ({children, onClick}: {children: React.ReactNode; onClick: () => void}) => (
        <button
            role='menuitem'
            onClick={onClick}
        >
            {children}
        </button>
    ),
    DropdownMenuSeparator: () => <hr data-testid='separator'/>,
}));

const intl = createIntl({locale: 'en', messages: {}});

const fetchPermissions = getParticipantPermissions as jest.Mock;

const flags = (over: Partial<Record<'can_mute' | 'can_deafen' | 'can_move' | 'can_kick', boolean>> = {}) => ({
    session_id: 'session-id',
    can_mute: true,
    can_deafen: true,
    can_move: true,
    can_kick: true,
    ...over,
});

const renderMenu = async (props: Partial<React.ComponentProps<typeof HostControlsMenu>> = {}) => {
    render(
        <RawIntlProvider value={intl}>
            <HostControlsMenu
                callID='channel-id'
                userID='user-id'
                sessionID='session-id'
                isMuted={false}
                isSharingScreen={false}
                isHandRaised={false}
                isHost={true}
                onRemove={jest.fn()}
                {...props}
            />
        </RawIntlProvider>,
    );

    // Let the permissions request answer.
    await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
};

const items = () => screen.queryAllByRole('menuitem').map((el) => el.textContent);

describe('HostControlsMenu', () => {
    beforeEach(() => {
        fetchPermissions.mockReset();
    });

    test('asks the server about the participant as it opens', async () => {
        fetchPermissions.mockResolvedValue(flags());
        await renderMenu();
        expect(fetchPermissions).toHaveBeenCalledWith('channel-id', 'session-id');
    });

    test('shows mute and remove when allowed', async () => {
        fetchPermissions.mockResolvedValue(flags());
        await renderMenu();
        expect(items()).toEqual(['Mute participant', 'Remove from call']);
        expect(screen.queryByTestId('separator')).not.toBeNull();
    });

    test('hides mute when the user may not mute them', async () => {
        fetchPermissions.mockResolvedValue(flags({can_mute: false}));
        await renderMenu();
        expect(items()).toEqual(['Remove from call']);
    });

    test('hides remove when the user may not remove them', async () => {
        fetchPermissions.mockResolvedValue(flags({can_kick: false}));
        await renderMenu();
        expect(items()).toEqual(['Mute participant']);
        expect(screen.queryByTestId('separator')).toBeNull();
    });

    test('hides both on someone at or above the user\'s rank, keeping the other controls', async () => {
        fetchPermissions.mockResolvedValue(flags({can_mute: false, can_deafen: false, can_move: false, can_kick: false}));
        await renderMenu({isHost: false, isHandRaised: true});
        expect(items()).toEqual(['Lower hand', 'Make host']);
        expect(screen.queryByTestId('separator')).toBeNull();
    });

    test('offers neither until the server answers', async () => {
        fetchPermissions.mockReturnValue(new Promise(() => {}));
        await renderMenu();
        expect(items()).toEqual([]);
    });

    test('offers both as before when the server can\'t tell', async () => {
        fetchPermissions.mockRejectedValue(Object.assign(new Error('not found'), {status_code: 404}));
        await renderMenu();
        expect(items()).toEqual(['Mute participant', 'Remove from call']);
    });

    test('offers both as before when the answer has no flags', async () => {
        fetchPermissions.mockResolvedValue({});
        await renderMenu();
        expect(items()).toEqual(['Mute participant', 'Remove from call']);
    });
});
