// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

/* eslint-disable formatjs/no-literal-string-in-jsx */

import {fireEvent, render, screen} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';

import {FusionDialog, FusionDialogPane} from './fusion_dialog';
import {FUSION_LAYER_ID} from './fusion_ui';

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

const layer = () => document.getElementById(FUSION_LAYER_ID)!;

function Message({onClose, onSubmit = jest.fn(), title = 'Call connection failed'}: {onClose: () => void; onSubmit?: () => void; title?: string}) {
    return (
        <IntlProvider locale='en'>
            <FusionDialog
                className='am-small amcalls-dialog'
                labelledBy='t'
                describedBy='d'
                onClose={onClose}
                onSubmit={onSubmit}
            >
                <FusionDialogPane
                    titleId='t'
                    textId='d'
                    title={title}
                    onClose={onClose}
                    actions={(
                        <>
                            <button
                                type='button'
                                className='am-btn'
                            >{'Cancel'}</button>
                            <button
                                type='submit'
                                className='am-btn am-primary'
                            >{'Okay'}</button>
                        </>
                    )}
                >
                    <span>{'Try to re-join the call.'}</span>
                </FusionDialogPane>
            </FusionDialog>
        </IntlProvider>
    );
}

describe('FusionDialog', () => {
    beforeEach(() => {
        document.body.innerHTML = `<div id="${FUSION_LAYER_ID}" class="am-layer"></div><button id="opener">open</button>`;
        setWebUI('fusion');
    });

    afterEach(() => setWebUI(undefined));

    test('draws Fusion\'s dialog in Fusion\'s layer: a scrim, the modal, its pane, title, text and buttons', () => {
        render(<Message onClose={jest.fn()}/>);

        const dialog = screen.getByRole('dialog');
        expect(layer().contains(dialog)).toBe(true);
        expect(dialog.parentElement!.className).toBe('am-scrim');
        expect(dialog.className).toBe('am-modal am-small amcalls-dialog');
        expect(dialog.tagName).toBe('FORM');
        expect(dialog.getAttribute('aria-modal')).toBe('true');
        expect(dialog.getAttribute('aria-labelledby')).toBe('t');
        expect(dialog.getAttribute('aria-describedby')).toBe('d');
        expect(dialog.querySelector('.am-pane h2#t')!.textContent).toBe('Call connection failed');
        expect(dialog.querySelector('.am-pane .amcalls-dialog-text#d')!.textContent).toBe('Try to re-join the call.');
        expect(dialog.querySelectorAll('.am-modal-actions .am-btn')).toHaveLength(2);
    });

    test('has Fusion\'s close button, with Fusion\'s X icon and tooltip', () => {
        const onClose = jest.fn();
        render(<Message onClose={onClose}/>);

        const close = screen.getByRole('button', {name: 'Close'});
        expect(close.className).toBe('am-icon-btn am-close');
        expect(close.getAttribute('data-am-tip')).toBe('Close');
        expect(close.querySelector('use')!.getAttribute('href')).toBe('#am-i-x');
        fireEvent.click(close);
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    test('focuses the main button, and gives the focus back once closed', () => {
        const opener = document.getElementById('opener')!;
        opener.focus();
        const {unmount} = render(<Message onClose={jest.fn()}/>);
        expect(document.activeElement).toBe(screen.getByRole('button', {name: 'Okay'}));
        unmount();
        expect(document.activeElement).toBe(opener);
    });

    test('Escape closes it, and doesn\'t reach what\'s under it', () => {
        const onClose = jest.fn();
        const under = jest.fn();
        document.addEventListener('keydown', under);
        render(<Message onClose={onClose}/>);
        fireEvent.keyDown(document.body, {key: 'Escape'});
        expect(onClose).toHaveBeenCalledTimes(1);
        expect(under).not.toHaveBeenCalled();
        document.removeEventListener('keydown', under);
    });

    test('with two dialogs open, Escape only closes the one on top', () => {
        const first = jest.fn();
        const second = jest.fn();
        render(
            <>
                <Message
                    onClose={first}
                    title='First'
                />
                <Message
                    onClose={second}
                    title='Second'
                />
            </>,
        );
        fireEvent.keyDown(document.body, {key: 'Escape'});
        expect(second).toHaveBeenCalledTimes(1);
        expect(first).not.toHaveBeenCalled();
    });

    test('a click beside it closes it, a click inside doesn\'t', () => {
        const onClose = jest.fn();
        render(<Message onClose={onClose}/>);
        fireEvent.mouseDown(screen.getByRole('dialog'));
        expect(onClose).not.toHaveBeenCalled();
        fireEvent.mouseDown(layer().querySelector('.am-scrim')!);
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    test('Enter (submitting the form) runs the action', () => {
        const onSubmit = jest.fn();
        render(
            <Message
                onClose={jest.fn()}
                onSubmit={onSubmit}
            />,
        );
        fireEvent.submit(screen.getByRole('dialog'));
        expect(onSubmit).toHaveBeenCalledTimes(1);
    });

    test('draws nothing without Fusion\'s layer (the classic UI)', () => {
        setWebUI('classic');
        render(<Message onClose={jest.fn()}/>);
        expect(screen.queryByRole('dialog')).toBeNull();
    });
});
