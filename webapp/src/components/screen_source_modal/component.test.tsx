// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {act, fireEvent, render, screen} from '@testing-library/react';
import React from 'react';
import {createIntl} from 'react-intl';
import {FUSION_LAYER_ID} from 'src/components/fusion/fusion_ui';

import ScreenSourceModal, {groupSources} from './component';

const intl = createIntl({locale: 'en', messages: {}, onError: () => null});

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

const source = (id: string, name: string) => ({id, name, thumbnailURL: `data:image/png;base64,${id}`});

const SOURCES = [
    source('window:42:0', 'Notes'),
    source('screen:1:0', 'Screen 1'),
    source('window:7:0', 'Calls Widget'),
    source('screen:2:0', 'Screen 2'),
];

type DesktopAPIStub = {getDesktopSources: jest.Mock; shareScreen: jest.Mock};

// The desktop app asks for the picker (show turns true), and Calls lists its sources.
const openPicker = async () => {
    const hide = jest.fn();
    const {rerender} = render(
        <ScreenSourceModal
            intl={intl}
            show={false}
            hideScreenSourceModal={hide}
        />,
    );
    await act(async () => {
        rerender(
            <ScreenSourceModal
                intl={intl}
                show={true}
                hideScreenSourceModal={hide}
            />,
        );
    });
    return hide;
};

describe('ScreenSourceModal', () => {
    let desktopAPI: DesktopAPIStub;

    beforeEach(() => {
        desktopAPI = {getDesktopSources: jest.fn().mockResolvedValue(SOURCES), shareScreen: jest.fn()};
        (window as unknown as {desktopAPI?: DesktopAPIStub}).desktopAPI = desktopAPI;
    });

    afterEach(() => {
        delete (window as unknown as {desktopAPI?: DesktopAPIStub}).desktopAPI;
        setWebUI(undefined);
    });

    test('groups the screens before the windows', () => {
        const {screens, windows} = groupSources(SOURCES);
        expect(screens.map((s) => s.name)).toEqual(['Screen 1', 'Screen 2']);
        expect(windows.map((s) => s.name)).toEqual(['Notes', 'Calls Widget']);
    });

    describe('under the Fusion UI', () => {
        beforeEach(() => {
            document.body.innerHTML = `<div id="${FUSION_LAYER_ID}" class="am-layer"></div>`;
            setWebUI('fusion');
        });

        test('is Fusion\'s own picker, in Fusion\'s layer: screens, then windows, without the call window', async () => {
            await openPicker();
            const dialog = screen.getByRole('dialog', {name: 'Choose what to share'});
            expect(document.getElementById(FUSION_LAYER_ID)!.contains(dialog)).toBe(true);
            expect(dialog.className).toBe('am-modal am-src-dialog');
            expect(Array.from(dialog.querySelectorAll('.am-src-head')).map((h) => h.textContent)).toEqual(['Screens', 'Windows']);

            const tiles = screen.getAllByRole('radio');
            expect(tiles.map((t) => t.querySelector('.am-src-name')!.textContent)).toEqual(['Screen 1', 'Screen 2', 'Notes']);
            expect(tiles.every((t) => t.className.startsWith('am-src'))).toBe(true);
            expect(tiles[0].getAttribute('data-am-tip')).toBe('Screen 1');
            expect(tiles[0].getAttribute('title')).toBeNull();
            expect(dialog.querySelector('.CompassIcon')).toBeNull();
            expect(dialog.querySelector('.screen-source-modal')).toBeNull();
        });

        test('shares the source picked', async () => {
            const hide = await openPicker();
            const tiles = screen.getAllByRole('radio');
            fireEvent.click(tiles[2]);
            expect(tiles[2].getAttribute('aria-checked')).toBe('true');
            expect(tiles[2].className).toBe('am-src am-on');
            expect(tiles[0].getAttribute('aria-checked')).toBe('false');

            const share = screen.getByRole('button', {name: 'Share'});
            expect(share.className).toBe('am-btn am-primary');
            fireEvent.click(share);
            expect(desktopAPI.shareScreen).toHaveBeenCalledWith('window:42:0', false);
            expect(hide).toHaveBeenCalled();
        });

        test('a double click shares that source at once', async () => {
            await openPicker();
            fireEvent.doubleClick(screen.getAllByRole('radio')[1]);
            expect(desktopAPI.shareScreen).toHaveBeenCalledWith('screen:2:0', false);
        });

        test('a click inside it doesn\'t close it', async () => {
            const hide = await openPicker();
            fireEvent.click(screen.getByRole('dialog'));
            fireEvent.mouseDown(screen.getByRole('dialog'));
            expect(hide).not.toHaveBeenCalled();
        });

        test.each([
            ['Escape', () => fireEvent.keyDown(document.body, {key: 'Escape'})],
            ['Cancel', () => fireEvent.click(screen.getByRole('button', {name: 'Cancel'}))],
            ['the close button', () => fireEvent.click(screen.getByRole('button', {name: 'Close'}))],
            ['a click beside it', () => fireEvent.mouseDown(document.querySelector('.am-scrim')!)],
        ])('%s closes it without sharing', async (_, close) => {
            const hide = await openPicker();
            close();
            expect(hide).toHaveBeenCalledTimes(1);
            expect(desktopAPI.shareScreen).not.toHaveBeenCalled();
            expect(screen.queryByRole('dialog')).toBeNull();
        });
    });

    describe('under the classic UI', () => {
        beforeEach(() => {
            document.body.innerHTML = '';
            setWebUI('classic');
        });

        test('keeps its classic picker', async () => {
            await openPicker();
            expect(document.querySelector('.am-scrim')).toBeNull();
            expect(document.getElementById('calls-screen-source-modal')!.className).toBe('screen-source-modal');
            expect(document.querySelectorAll('.screen-source')).toHaveLength(3);
            expect(document.querySelector('.screen-source-modal-close .CompassIcon')).not.toBeNull();
        });
    });
});
