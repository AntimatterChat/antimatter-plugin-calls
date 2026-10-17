// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Channel} from '@mattermost/types/channels';
import {fireEvent, render, screen} from '@testing-library/react';
import React from 'react';
import {createIntl} from 'react-intl';
import {FUSION_LAYER_ID} from 'src/components/fusion/fusion_ui';

import SwitchCallModal from './component';

const intl = createIntl({locale: 'en', messages: {}, onError: () => null});

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

const channel = (id: string, name: string) => ({id, type: 'O', display_name: name}) as Channel;

const renderModal = () => {
    const props = {
        intl,
        show: true,
        connectedChannel: channel('town-square', 'Town Square'),
        currentChannel: channel('off-topic', 'Off-topic'),
        connectedDMUser: undefined,
        currentDMUser: undefined,
        targetChannelID: 'off-topic',
        targetCallID: 'call-id',
        hideSwitchCallModal: jest.fn(),
        dismissIncomingCallNotification: jest.fn(),
    };
    render(<SwitchCallModal {...props}/>);
    return props;
};

describe('SwitchCallModal', () => {
    afterEach(() => setWebUI(undefined));

    describe('under the Fusion UI', () => {
        beforeEach(() => {
            document.body.innerHTML = `<div id="${FUSION_LAYER_ID}" class="am-layer"></div>`;
            setWebUI('fusion');
        });

        test('is Fusion\'s dialog in Fusion\'s layer, with Cancel and the primary action', () => {
            renderModal();
            const dialog = screen.getByRole('dialog');
            expect(document.getElementById(FUSION_LAYER_ID)!.contains(dialog)).toBe(true);
            expect(dialog.className).toBe('am-modal am-small amcalls-dialog');
            expect(dialog.querySelector('h2')!.textContent).toBe('You\'re already in a call');
            expect(dialog.querySelector('.amcalls-dialog-text')!.textContent).toBe('You\'re already in a call in Town Square. Do you want to leave and join a call in Off-topic?');
            expect(screen.getByRole('button', {name: 'Cancel'}).className).toBe('am-btn');
            expect(screen.getByRole('button', {name: 'Leave and join new call'}).className).toBe('am-btn am-primary');
            expect(dialog.querySelector('.CompassIcon')).toBeNull();
            expect(screen.getByRole('button', {name: 'Close'}).querySelector('use')!.getAttribute('href')).toBe('#am-i-x');
        });

        test('leaving joins the new call', () => {
            const postMessage = jest.spyOn(window, 'postMessage').mockImplementation(() => null);
            const props = renderModal();
            fireEvent.click(screen.getByRole('button', {name: 'Leave and join new call'}));
            expect(props.dismissIncomingCallNotification).toHaveBeenCalledWith('off-topic', 'call-id');
            expect(props.hideSwitchCallModal).toHaveBeenCalled();
            expect(postMessage).toHaveBeenCalledWith({type: 'connectCall', channelID: 'off-topic'}, window.origin);
            postMessage.mockRestore();
        });

        test('Escape, Cancel or a click beside it closes it; a click inside doesn\'t', () => {
            const props = renderModal();
            fireEvent.mouseDown(screen.getByRole('dialog'));
            fireEvent.click(screen.getByRole('dialog'));
            expect(props.hideSwitchCallModal).not.toHaveBeenCalled();

            fireEvent.keyDown(document.body, {key: 'Escape'});
            expect(props.hideSwitchCallModal).toHaveBeenCalledTimes(1);
            fireEvent.click(screen.getByRole('button', {name: 'Cancel'}));
            expect(props.hideSwitchCallModal).toHaveBeenCalledTimes(2);
            fireEvent.mouseDown(document.querySelector('.am-scrim')!);
            expect(props.hideSwitchCallModal).toHaveBeenCalledTimes(3);
        });
    });

    describe('under the classic UI', () => {
        beforeEach(() => {
            document.body.innerHTML = '';
            setWebUI('classic');
        });

        test('keeps its classic dialog', () => {
            renderModal();
            expect(document.querySelector('.am-scrim')).toBeNull();
            expect(document.getElementById('calls-switch-call-modal')).not.toBeNull();
            expect(document.querySelector('.switch-call-modal-join')!.textContent).toBe('Leave and join new call');
            expect(document.querySelector('.CompassIcon.icon-close')).not.toBeNull();
        });
    });
});
