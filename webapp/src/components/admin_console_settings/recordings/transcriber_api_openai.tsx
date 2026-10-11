// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {ChangeEvent} from 'react';
import {defineMessage, MessageDescriptor, useIntl} from 'react-intl';
import {useSelector} from 'react-redux';
import {LabelRow, leftCol, RadioInput, RadioInputLabel, rightCol} from 'src/components/admin_console_settings/common';
import {TRANSCRIBE_API_OPENAI} from 'src/constants';
import {callsConfig, callsConfigEnvOverrides, transcribeAPI, transcriptionsEnabled} from 'src/selectors';
import {CustomComponentProps} from 'src/types/antimatter-webapp';

type CallsConfigKey = 'TranscribeAPIOpenAIURL' | 'TranscribeAPIOpenAIModel' | 'TranscribeAPIOpenAIKey' | 'SummarizeTranscriptions';

// useAPISetting gives what a setting of the OpenAI-compatible speech-to-text API needs: whether it shows (the API is
// chosen), and its value and state when an environment variable sets it.
function useAPISetting(props: CustomComponentProps, key: CallsConfigKey) {
    const transcriptionEnabled = useSelector(transcriptionsEnabled);
    const api = useSelector(transcribeAPI);
    const config = useSelector(callsConfig) as unknown as Record<string, unknown>;
    const overrides = useSelector(callsConfigEnvOverrides);
    const overridden = key in overrides;
    return {
        visible: transcriptionEnabled && api === TRANSCRIBE_API_OPENAI,
        overridden,
        value: overridden ? config[key] : props.value,
        disabled: props.disabled || overridden,
    };
}

const overriddenMessage = defineMessage({defaultMessage: 'This setting has been set through an environment variable. It cannot be changed through the System Console.'});

function textSetting(key: CallsConfigKey, label: MessageDescriptor, help: MessageDescriptor, placeholder: string, secret = false) {
    const Setting = (props: CustomComponentProps) => {
        const {formatMessage} = useIntl();
        const {visible, overridden, value, disabled} = useAPISetting(props, key);
        if (!visible) {
            return null;
        }
        return (
            <div
                data-testid={props.id}
                className='form-group'
            >
                <div className={'control-label ' + leftCol}>
                    <LabelRow>
                        <label
                            data-testid={props.id + 'label'}
                            htmlFor={props.id}
                        >
                            {formatMessage(label)}
                        </label>
                    </LabelRow>
                </div>
                <div className={rightCol}>
                    <input
                        data-testid={props.id + 'input'}
                        id={props.id}
                        className={disabled ? 'form-control disabled' : 'form-control'}
                        type={secret ? 'password' : 'input'}
                        autoComplete='off'
                        placeholder={placeholder}
                        value={String(value ?? '')}
                        onChange={(e: ChangeEvent<HTMLInputElement>) => props.onChange(props.id, e.target.value)}
                        disabled={disabled}
                    />
                    <div
                        data-testid={props.id + 'help-text'}
                        className='help-text'
                    >
                        {formatMessage(help)}
                    </div>
                    {overridden && <div className='alert alert-warning'>{formatMessage(overriddenMessage)}</div>}
                </div>
            </div>
        );
    };
    return Setting;
}

export const TranscribeAPIOpenAIURL = textSetting(
    'TranscribeAPIOpenAIURL',
    defineMessage({defaultMessage: 'Speech-to-text API URL'}),
    defineMessage({defaultMessage: 'The base URL of an OpenAI-compatible speech-to-text API, up to and including /v1. Each participant\'s app sends its own audio while a call is transcribed: no job service or recording is needed.'}),
    'https://stt.example.com/v1',
);

export const TranscribeAPIOpenAIModel = textSetting(
    'TranscribeAPIOpenAIModel',
    defineMessage({defaultMessage: 'Speech-to-text model'}),
    defineMessage({defaultMessage: 'The model as the API names it, e.g. whisper-1.'}),
    'whisper-1',
);

export const TranscribeAPIOpenAIKey = textSetting(
    'TranscribeAPIOpenAIKey',
    defineMessage({defaultMessage: 'Speech-to-text API key'}),
    defineMessage({defaultMessage: 'Sent as a bearer token. Leave it blank if the API needs none.'}),
    '',
    true,
);

export const SummarizeTranscriptions = (props: CustomComponentProps) => {
    const {formatMessage} = useIntl();
    const {visible, overridden, value, disabled} = useAPISetting(props, 'SummarizeTranscriptions');
    if (!visible) {
        return null;
    }
    const checked = value === true || value === 'true';
    const handleChange = (e: ChangeEvent<HTMLInputElement>) => props.onChange(props.id, e.target.value === 'true');
    return (
        <div
            data-testid={props.id}
            className='form-group'
        >
            <label className={'control-label ' + leftCol}>
                {formatMessage({defaultMessage: 'Summarize transcriptions'})}
            </label>
            <div className={rightCol}>
                {[true, false].map((option) => (
                    <RadioInputLabel
                        key={String(option)}
                        $disabled={disabled}
                    >
                        <RadioInput
                            data-testid={props.id + String(option)}
                            type='radio'
                            value={String(option)}
                            id={props.id + String(option)}
                            name={props.id + String(option)}
                            checked={checked === option}
                            onChange={handleChange}
                            disabled={disabled}
                        />
                        {option ? formatMessage({defaultMessage: 'True'}) : formatMessage({defaultMessage: 'False'})}
                    </RadioInputLabel>
                ))}
                <div
                    data-testid={props.id + 'help-text'}
                    className='help-text'
                >
                    {formatMessage({defaultMessage: 'When set to true, the default agent of the Agents plugin posts a summary of each transcription in the call\'s thread.'})}
                </div>
                {overridden && <div className='alert alert-warning'>{formatMessage(overriddenMessage)}</div>}
            </div>
        </div>
    );
};
