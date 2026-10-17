// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import React from 'react';
import {IntlProvider} from 'react-intl';
import {Provider} from 'react-redux';
import {mockStore} from 'src/testUtils';

import EnableVideo from './enable_video';

describe('EnableVideo', () => {
    const baseProps = {
        id: 'EnableVideo',
        label: 'Enable video',
        helpText: null,
        value: 'true',
        disabled: false,
        setByEnv: false,
        onChange: jest.fn(),
        saveAction: jest.fn(),
        registerSaveAction: jest.fn(),
        unRegisterSaveAction: jest.fn(),
        setSaveNeeded: jest.fn(),
        config: {},
        license: {},
        cancelSubmit: () => {},
        showConfirm: false,
    };

    const renderComponent = (props = {}, storeOverrides = {}) => {
        const store = mockStore({
            'plugins-com.mattermost.calls': {
                callsConfig: {
                    EnableVideo: true,
                },
                callsConfigEnvOverrides: {},
            },
            ...storeOverrides,
        });

        return render(
            <Provider store={store}>
                <IntlProvider locale='en'>
                    <EnableVideo
                        {...baseProps}
                        {...props}
                    />
                </IntlProvider>
            </Provider>,
        );
    };

    it('says video goes wherever calls are', () => {
        renderComponent();

        expect(screen.getByText('Enable video')).toBeInTheDocument();
        expect(screen.getByText('When set to true, participants can turn on their camera in calls, wherever calls are enabled. In voice channels, the Voice Channels app\'s Allow video in voice channels setting decides.')).toBeInTheDocument();
        expect(screen.getByTestId('EnableVideotrue')).toBeChecked();
    });

    it('is on when the setting was never saved, as the server\'s default', () => {
        // eslint-disable-next-line no-undefined
        renderComponent({value: undefined});

        expect(screen.getByTestId('EnableVideotrue')).toBeChecked();
        expect(screen.getByTestId('EnableVideofalse')).not.toBeChecked();
    });

    it('keeps an admin\'s choice to turn it off', () => {
        renderComponent({value: false});
        expect(screen.getByTestId('EnableVideofalse')).toBeChecked();

        renderComponent({value: 'false'});
        expect(screen.getAllByTestId('EnableVideofalse')[1]).toBeChecked();
    });

    it('calls onChange with a boolean', async () => {
        const onChange = jest.fn();
        renderComponent({onChange});

        await userEvent.click(screen.getByTestId('EnableVideofalse'));
        expect(onChange).toHaveBeenCalledWith('EnableVideo', false);
    });

    it('shows the value set through an environment variable, and locks it', () => {
        renderComponent({value: 'true'}, {
            'plugins-com.mattermost.calls': {
                callsConfig: {
                    EnableVideo: false,
                },
                callsConfigEnvOverrides: {
                    EnableVideo: 'false',
                },
            },
        });

        expect(screen.getByTestId('EnableVideofalse')).toBeChecked();
        expect(screen.getByTestId('EnableVideotrue')).toBeDisabled();
        expect(screen.getByText('This setting has been set through an environment variable. It cannot be changed through the System Console.')).toBeInTheDocument();
    });
});
