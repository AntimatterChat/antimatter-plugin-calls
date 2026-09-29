// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {useIntl} from 'react-intl';
import {useSelector} from 'react-redux';
import {
    SectionTitle,
    UnavailableSubtitle,
} from 'src/components/admin_console_settings/common';
import {recordingsEnabled} from 'src/selectors';

export default function CallTranscriptionsSection(props: {settingsList: React.ReactNode[]}) {
    const {formatMessage} = useIntl();
    const recordingEnabled = useSelector(recordingsEnabled);

    const subtitleMsg = recordingEnabled ? formatMessage({defaultMessage: 'Allows calls to be transcribed to text files. Recordings must be enabled'}) :
        formatMessage({defaultMessage: 'Allows calls to be transcribed to text files. To enable call transcriptions, recordings must be enabled first'});

    const subtitle = recordingEnabled ? (
        <div className='section-subtitle'>
            {subtitleMsg}
        </div>
    ) : (
        <UnavailableSubtitle className='section-subtitle'>
            {subtitleMsg}
        </UnavailableSubtitle>
    );

    return (
        <div
            className='config-section'
            data-testid={'calls-transcriptions-section'}
        >
            <div className='admin-console__wrapper'>
                <div className='admin-console__content'>
                    <div className='section-header'>
                        <SectionTitle className='section-title'>
                            {formatMessage({defaultMessage: 'Call transcriptions'})}
                        </SectionTitle>
                        {subtitle}
                    </div>
                    {recordingEnabled &&
                    <div className='section-body'>
                        {props.settingsList}
                    </div>
                    }
                </div>
            </div>
        </div>
    );
}
