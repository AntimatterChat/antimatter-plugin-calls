// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useState} from 'react';
import {useIntl} from 'react-intl';
import {FusionSwitchRow, useFusionSettings} from 'src/components/user_settings/fusion_section';
import {STORAGE_CALLS_SHARE_AUDIO_WITH_SCREEN} from 'src/constants';
import {untranslatable} from 'src/utils';

export default function ScreenSharingSettingsSection() {
    const {formatMessage} = useIntl();
    const [active, setActive] = useState(false);

    const title = formatMessage({defaultMessage: 'Screen sharing settings'});
    const description = formatMessage({defaultMessage: 'Configure your screen sharing settings.'});
    const editLabel = formatMessage({defaultMessage: 'Edit'});

    const [shareAudio, setShareAudio] = useState(window.localStorage.getItem(STORAGE_CALLS_SHARE_AUDIO_WITH_SCREEN) || 'on');

    const onLabel = formatMessage({defaultMessage: 'On'});
    const offLabel = formatMessage({defaultMessage: 'Off'});
    const shareAudioSettingTitle = formatMessage({defaultMessage: 'Share sound with screen'});
    const shareAudioSettingHelpText = formatMessage({defaultMessage: 'When enabled, audio from your browser tab or system (depending on what\'s shared) will be included with the screen content.'});

    const handleSave = () => {
        setActive(false);
    };

    const saveShareAudio = (value: string) => {
        setShareAudio(value);
        window.localStorage.setItem(STORAGE_CALLS_SHARE_AUDIO_WITH_SCREEN, value);
    };

    const handleShareAudioChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        saveShareAudio(e.target.value);
    };

    const [sectionRef, fusion] = useFusionSettings();

    // Under Fusion, the setting is one switch, saved when turned on or off, as Fusion's own settings.
    if (fusion) {
        return (
            <FusionSwitchRow
                rowRef={sectionRef}
                title={shareAudioSettingTitle}
                text={shareAudioSettingHelpText}
                checked={shareAudio === 'on'}
                onChange={(on) => saveShareAudio(on ? 'on' : 'off')}
            />
        );
    }

    if (!active) {
        return (
            <div
                ref={sectionRef}
                className='section-min'
                onClick={() => setActive(!active)}
            >
                <div className='secion-min__header'>
                    <h4 className='section-min__title'>
                        <span>{title}</span>
                    </h4>
                    <button
                        className='color--link style--none section-min__edit'
                        aria-labelledby=''
                        aria-expanded={active}
                    >
                        <i
                            className='icon-pencil-outline'
                            title={editLabel}
                        />
                        <span>{editLabel}</span>
                    </button>
                </div>
                <div className='section-min__describe'>
                    <span>{description}</span>
                </div>
            </div>
        );
    }

    return (
        <section
            ref={sectionRef}
            className='section-max form-horizontal'
        >
            <h4
                id='settingTitle'
                className='col-sm-12 section-title'
            ><span>{shareAudioSettingTitle}</span></h4>
            <div className='sectionContent col-sm-10 col-sm-offset-2'>
                <div
                    tabIndex={-1}
                    className='setting-list'
                >
                    <div className='setting-list-item'>
                        <fieldset>
                            <legend className='form-legend hidden-label'>{untranslatable('enableAudioSharing')}</legend>
                            <div className='radio'>
                                <label>
                                    <input
                                        type='radio'
                                        name='enableAudioSharing'
                                        value='on'
                                        onChange={handleShareAudioChange}
                                        checked={shareAudio === 'on'}
                                    />
                                    {onLabel}
                                </label><br/>
                            </div>
                            <div className='radio'>
                                <label>
                                    <input
                                        type='radio'
                                        name='enableAudioSharing'
                                        value='off'
                                        onChange={handleShareAudioChange}
                                        checked={shareAudio === 'off'}
                                    />
                                    {offLabel}
                                </label><br/>
                            </div>
                            <div className='mt-5'>
                                <p>{shareAudioSettingHelpText}</p>
                            </div>
                        </fieldset>
                    </div>
                    <div className='setting-list-item'>
                        <hr/>
                        <button
                            type='submit'
                            className='btn btn-primary'
                            onClick={handleSave}
                        >
                            {formatMessage({defaultMessage: 'Save'})}
                        </button>
                        <button
                            className='btn btn-tertiary'
                            onClick={() => setActive(false)}
                        >
                            {formatMessage({defaultMessage: 'Cancel'})}
                        </button>
                    </div>
                </div>
            </div>
        </section>
    );
}

