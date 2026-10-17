// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {act, fireEvent, render, screen} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';
import {FUSION_LAYER_ID} from 'src/components/fusion/fusion_ui';
import {
    STORAGE_CALLS_DEFAULT_AUDIO_INPUT_KEY,
    STORAGE_CALLS_MIRROR_VIDEO_KEY,
    STORAGE_CALLS_SHARE_AUDIO_WITH_SCREEN,
} from 'src/constants';

import AudioDevicesSettingsSection from './audio_devices_settings_section';
import ScreenSharingSettingsSection from './screen_sharing_settings_section';
import VideoDevicesSettingsSection from './video_devices_settings_section';

const setWebUI = (ui?: 'classic' | 'fusion') => {
    window.antimatterWebUI = ui;
    if (ui) {
        document.documentElement.dataset.amWebUi = ui;
    } else {
        delete document.documentElement.dataset.amWebUi;
    }
};

const device = (kind: MediaDeviceKind, deviceId: string, label: string) => ({kind, deviceId, label, groupId: ''}) as MediaDeviceInfo;

const DEVICES = [
    device('audioinput', 'mic-1', 'Desk microphone'),
    device('audioinput', 'mic-2', 'Headset microphone'),
    device('audiooutput', 'out-1', 'Speakers'),
    device('videoinput', 'cam-1', 'Webcam'),
];

const stream = () => ({getTracks: () => [], getVideoTracks: () => [{stop: jest.fn()}]});

// Where Fusion's settings dialog draws an app's settings: in Fusion's layer.
const inFusionSettings = () => {
    const pane = document.createElement('div');
    pane.className = 'am-plugin-pane user-settings';
    document.getElementById(FUSION_LAYER_ID)!.appendChild(pane);
    return pane;
};

const renderSection = (section: React.ReactElement, container?: HTMLElement) => render(
    <IntlProvider locale='en'>{section}</IntlProvider>,
    container ? {container} : undefined,
);

describe('Calls\' settings', () => {
    beforeEach(() => {
        document.body.innerHTML = `<div id="${FUSION_LAYER_ID}" class="am-layer"></div>`;
        window.localStorage.clear();
        Object.defineProperty(navigator, 'mediaDevices', {
            configurable: true,
            value: {
                getUserMedia: jest.fn().mockResolvedValue(stream()),
                enumerateDevices: jest.fn().mockResolvedValue(DEVICES),
                addEventListener: jest.fn(),
                removeEventListener: jest.fn(),
            },
        });
    });

    afterEach(() => setWebUI(undefined));

    describe('in the Fusion UI\'s settings dialog', () => {
        beforeEach(() => setWebUI('fusion'));

        test('the audio devices are a row with Edit, then Fusion\'s fields, Cancel and Save', async () => {
            renderSection(<AudioDevicesSettingsSection/>, inFusionSettings());

            const row = document.querySelector('.am-toggle-row')!;
            expect(row.querySelector('.am-txt b')!.textContent).toBe('Audio devices');
            expect(document.querySelector('.section-min')).toBeNull();
            const edit = screen.getByRole('button', {name: 'Edit'});
            expect(edit.className).toBe('am-btn');

            await act(async () => {
                fireEvent.click(edit);
            });

            const section = screen.getByRole('region', {name: 'Audio devices'});
            expect(section.querySelector('h3')!.className).toBe('am-set-section');
            const fields = section.querySelectorAll('label.am-field');
            expect(Array.from(fields).map((f) => f.querySelector('span')!.textContent)).toEqual(['Microphone input', 'Speaker output']);
            expect(document.querySelector('.react-select__control')).toBeNull();

            const mic = screen.getByRole('combobox', {name: 'Microphone input'}) as HTMLSelectElement;
            expect(Array.from(mic.options).map((o) => o.textContent)).toEqual(['Desk microphone', 'Headset microphone']);
            fireEvent.change(mic, {target: {value: 'mic-2'}});

            expect(screen.getByRole('button', {name: 'Cancel'}).className).toBe('am-btn');
            const save = screen.getByRole('button', {name: 'Save'});
            expect(save.className).toBe('am-btn am-primary');
            fireEvent.click(save);
            expect(JSON.parse(window.localStorage.getItem(STORAGE_CALLS_DEFAULT_AUDIO_INPUT_KEY)!)).toEqual({deviceId: 'mic-2', label: 'Headset microphone'});
            expect(screen.queryByRole('region', {name: 'Audio devices'})).toBeNull();
        });

        test('sharing sound with the screen is Fusion\'s switch, saved at once', () => {
            renderSection(<ScreenSharingSettingsSection/>, inFusionSettings());

            const toggle = screen.getByRole('switch', {name: 'Share sound with screen'});
            expect(toggle.className).toBe('am-switch');
            expect(toggle.getAttribute('aria-checked')).toBe('true');
            fireEvent.click(toggle);
            expect(toggle.getAttribute('aria-checked')).toBe('false');
            expect(window.localStorage.getItem(STORAGE_CALLS_SHARE_AUDIO_WITH_SCREEN)).toBe('off');
            fireEvent.click(toggle);
            expect(window.localStorage.getItem(STORAGE_CALLS_SHARE_AUDIO_WITH_SCREEN)).toBe('on');
        });

        test('the video devices get Fusion\'s field and switches', async () => {
            renderSection(<VideoDevicesSettingsSection/>, inFusionSettings());
            await act(async () => {
                fireEvent.click(screen.getByRole('button', {name: 'Edit'}));
            });

            expect(screen.getByRole('combobox', {name: 'Camera input'}).closest('.am-field')).not.toBeNull();
            const mirror = screen.getByRole('switch', {name: 'Mirror video'});
            expect(mirror.className).toBe('am-switch');
            fireEvent.click(mirror);
            fireEvent.click(screen.getByRole('button', {name: 'Save'}));
            expect(window.localStorage.getItem(STORAGE_CALLS_MIRROR_VIDEO_KEY)).toBe('true');
        });

        test('the classic settings dialog (outside Fusion\'s layer) keeps the classic sections', () => {
            renderSection(<AudioDevicesSettingsSection/>);
            expect(document.querySelector('.section-min')).not.toBeNull();
            expect(document.querySelector('.am-toggle-row')).toBeNull();
        });
    });

    describe('under the classic UI', () => {
        beforeEach(() => setWebUI('classic'));

        test('keep the classic sections', () => {
            renderSection(
                <>
                    <AudioDevicesSettingsSection/>
                    <ScreenSharingSettingsSection/>
                </>,
                inFusionSettings(),
            );
            expect(document.querySelectorAll('.section-min')).toHaveLength(2);
            expect(document.querySelector('[class*="am-"]:not(.am-layer, .am-plugin-pane)')).toBeNull();
        });
    });
});
