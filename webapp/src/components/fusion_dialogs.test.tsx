// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {fireEvent, render, screen} from '@testing-library/react';
import React from 'react';
import {defineMessage, IntlProvider} from 'react-intl';
import {Provider} from 'react-redux';
import {insecureContextErr, rtcPeerErr} from 'src/client';
import {mockStore} from 'src/testUtils';

jest.mock('src/actions', () => ({
    stopCallRecording: jest.fn(),
}));

/* eslint-disable import/order */
import {stopCallRecording} from 'src/actions';
import {CallErrorModal} from 'src/components/call_error_modal';
import {StopRecordingConfirmation} from 'src/components/expanded_view/stop_recording_confirmation';
import {FUSION_LAYER_ID} from 'src/components/fusion/fusion_ui';
import {GenericErrorModal} from 'src/components/generic_error_modal';
import {CallsInTestModeModal} from 'src/components/modals';
/* eslint-enable import/order */

// Calls' dialogs (connection errors, the /call errors, test mode, stopping a recording) are all GenericModal, which
// draws Fusion's dialog in Fusion's layer under the Fusion UI, and keeps react-bootstrap's under the classic UI.

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

const layer = () => document.getElementById(FUSION_LAYER_ID)!;

const store = mockStore({'plugins-com.mattermost.calls': {callsConfig: {EnableTranscriptions: false}}});

// As the web app's modal controller opens them: onExited forgets the dialog, onHide asks it to close.
const controller = () => ({onExited: jest.fn(), onHide: jest.fn()});

const renderDialog = (dialog: React.ReactElement) => render(
    <Provider store={store}>
        <IntlProvider locale='en'>{dialog}</IntlProvider>
    </Provider>,
);

const title = defineMessage({defaultMessage: 'Unable to start or join call'});
const message = defineMessage({defaultMessage: 'Calls are disabled in this channel.'});

describe('Calls\' dialogs under the Fusion UI', () => {
    beforeEach(() => {
        document.body.innerHTML = `<div id="${FUSION_LAYER_ID}" class="am-layer"></div>`;
        setWebUI('fusion');
    });

    afterEach(() => setWebUI(undefined));

    test('a /call error is Fusion\'s dialog in Fusion\'s layer, with one primary button', () => {
        const props = controller();
        renderDialog(
            <GenericErrorModal
                {...props}
                title={title}
                message={message}
            />,
        );

        const dialog = screen.getByRole('dialog');
        expect(layer().contains(dialog)).toBe(true);
        expect(dialog.className).toContain('am-modal am-small amcalls-dialog');
        expect(dialog.querySelector('h2')!.textContent).toBe('Unable to start or join call');
        expect(dialog.querySelector('.amcalls-dialog-text')!.textContent).toBe('Calls are disabled in this channel.');
        expect(document.querySelector('.modal')).toBeNull();

        const buttons = dialog.querySelectorAll('.am-modal-actions button');
        expect(buttons).toHaveLength(1);
        expect(buttons[0].className).toBe('am-btn am-primary');
        expect(buttons[0].textContent).toBe('Understood');
        fireEvent.click(buttons[0]);
        expect(props.onExited).toHaveBeenCalledTimes(1);
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    test('Escape closes it, and the modal controller forgets it', () => {
        const props = controller();
        renderDialog(
            <GenericErrorModal
                {...props}
                title={title}
                message={message}
            />,
        );
        fireEvent.keyDown(document.body, {key: 'Escape'});
        expect(props.onExited).toHaveBeenCalledTimes(1);
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    test('so does its close button', () => {
        const props = controller();
        renderDialog(
            <GenericErrorModal
                {...props}
                title={title}
                message={message}
            />,
        );
        fireEvent.click(screen.getByRole('button', {name: 'Close'}));
        expect(props.onExited).toHaveBeenCalledTimes(1);
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    test('a connection error says what happened and offers to re-join', () => {
        renderDialog(
            <CallErrorModal
                {...controller()}
                err={rtcPeerErr}
                channelID='channel-id'
            />,
        );
        const dialog = screen.getByRole('dialog');
        expect(layer().contains(dialog)).toBe(true);
        expect(dialog.querySelector('h2')!.textContent).toBe('Call connection failed');
        expect(screen.getByRole('link', {name: 're-join'})).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Okay'}).className).toBe('am-btn am-primary');
    });

    test('the insecure context error has no illustration, only Fusion\'s icons', () => {
        renderDialog(
            <CallErrorModal
                {...controller()}
                err={insecureContextErr}
            />,
        );
        const dialog = screen.getByRole('dialog');
        expect(dialog.querySelector('h2')!.textContent).toBe('Calls can\'t be initiated in an insecure context');
        expect(Array.from(dialog.querySelectorAll('svg')).every((svg) => svg.getAttribute('class')?.startsWith('am-ic'))).toBe(true);
        expect(dialog.querySelector('path')).toBeNull();
        expect(screen.getByRole('button', {name: 'Cancel'}).className).toBe('am-btn');
        expect(screen.getByRole('button', {name: 'Learn more'}).className).toBe('am-btn am-primary');
    });

    test('test mode is Fusion\'s dialog', () => {
        renderDialog(<CallsInTestModeModal {...controller()}/>);
        const dialog = screen.getByRole('dialog');
        expect(layer().contains(dialog)).toBe(true);
        expect(dialog.querySelector('h2')!.textContent).toBe('Calls is not currently enabled');
    });

    test('stopping a recording asks with a red action, which stops it', () => {
        const props = controller();
        renderDialog(
            <StopRecordingConfirmation
                {...props}
                channelID='channel-id'
            />,
        );
        const dialog = screen.getByRole('dialog');
        expect(layer().contains(dialog)).toBe(true);
        expect(screen.getByRole('button', {name: 'Cancel'}).className).toBe('am-btn');
        const stop = screen.getByRole('button', {name: 'Stop recording'});
        expect(stop.className).toBe('am-btn am-danger');
        expect(document.activeElement).toBe(stop);
        fireEvent.click(stop);
        expect(stopCallRecording).toHaveBeenCalledWith('channel-id');
        expect(props.onExited).toHaveBeenCalledTimes(1);
    });

    test('Cancel doesn\'t stop the recording', () => {
        const props = controller();
        renderDialog(
            <StopRecordingConfirmation
                {...props}
                channelID='channel-id'
            />,
        );
        fireEvent.click(screen.getByRole('button', {name: 'Cancel'}));
        expect(stopCallRecording).not.toHaveBeenCalled();
        expect(props.onExited).toHaveBeenCalledTimes(1);
    });
});

describe('Calls\' dialogs under the classic UI', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        setWebUI('classic');
    });

    afterEach(() => setWebUI(undefined));

    test('keep react-bootstrap\'s dialog and the classic buttons', () => {
        renderDialog(
            <GenericErrorModal
                {...controller()}
                title={title}
                message={message}
            />,
        );
        expect(document.querySelector('.am-scrim')).toBeNull();
        expect(document.querySelector('.am-btn')).toBeNull();
        expect(document.querySelector('.modal-title')!.textContent).toBe('Unable to start or join call');
        expect(screen.getByTestId('modal-confirm-button').textContent).toBe('Understood');
    });
});
