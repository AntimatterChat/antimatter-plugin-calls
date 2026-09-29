// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {render, screen} from '@testing-library/react';
import React from 'react';
import {IntlProvider} from 'react-intl';
import {Provider} from 'react-redux';
import {mockStore} from 'src/testUtils';
import {untranslatable} from 'src/utils';

import CallRecordingsSection from './call_recordings';

describe('CallRecordingsSection', () => {
    const settingsList = [
        <div
            key='setting1'
            data-testid='setting1'
        >{untranslatable('Setting 1')}</div>,
        <div
            key='setting2'
            data-testid='setting2'
        >{untranslatable('Setting 2')}</div>,
    ];

    const renderComponent = (storeOverrides = {}) => {
        const store = mockStore({
            'plugins-com.mattermost.calls': {
                callsConfig: {},
            },
            ...storeOverrides,
        });

        return render(
            <Provider store={store}>
                <IntlProvider locale='en'>
                    <CallRecordingsSection settingsList={settingsList}/>
                </IntlProvider>
            </Provider>,
        );
    };

    it('should render correctly with settings list for enterprise', () => {
        renderComponent();

        expect(screen.getByText('Call recordings')).toBeInTheDocument();
        expect(screen.getByText('Recordings include the entire call window view along with participants’ audio track and any shared screen video. Recordings are stored in Antimatter')).toBeInTheDocument();
        expect(screen.getByTestId('setting1')).toBeInTheDocument();
        expect(screen.getByTestId('setting2')).toBeInTheDocument();
    });
});
