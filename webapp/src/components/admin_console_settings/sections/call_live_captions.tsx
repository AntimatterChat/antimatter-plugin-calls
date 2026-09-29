// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {useIntl} from 'react-intl';
import {useSelector} from 'react-redux';
import {
    SectionTitle,
    UnavailableSubtitle,
} from 'src/components/admin_console_settings/common';
import {recordingsEnabled, transcriptionsEnabled} from 'src/selectors';

export default function CallLiveCaptionsSection(props: {settingsList: React.ReactNode[]}) {
    const {formatMessage} = useIntl();
    const recordingEnabled = useSelector(recordingsEnabled);
    const transcriptionEnabled = useSelector(transcriptionsEnabled);

    const subtitleMsg = recordingEnabled && transcriptionEnabled ? formatMessage({defaultMessage: 'Displays spoken words as text captions during a call. Recordings and transcriptions must be enabled'}) :
        formatMessage({defaultMessage: 'Displays spoken words as text captions during a call. To enable live captions, recordings and transcriptions must be enabled first'});

    const subtitle = recordingEnabled && transcriptionEnabled ? (
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
            data-testid={'calls-live-captions-section'}
        >
            <div className='admin-console__wrapper'>
                <div className='admin-console__content'>
                    <div className='section-header'>
                        <SectionTitle className='section-title'>
                            {formatMessage({defaultMessage: 'Live captions'})}
                        </SectionTitle>
                        {subtitle}
                    </div>
                    { recordingEnabled && transcriptionEnabled &&
                    <div className='section-body'>
                        {props.settingsList}
                    </div>
                    }
                </div>
            </div>
        </div>
    );
}
