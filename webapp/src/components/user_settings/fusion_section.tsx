// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import 'src/components/fusion/fusion.css';

import React, {useCallback, useId, useState} from 'react';
import {useIntl} from 'react-intl';
import {isFusion} from 'src/components/fusion/fusion_ui';

import type {SelectOption} from './common';

/**
 * useFusionSettings tells whether a settings section is drawn in the Fusion UI's settings dialog (in Fusion's layer
 * or app, where Fusion's classes apply), from the element the returned ref is set on. The classic settings dialog,
 * which the desktop app can open under Fusion too (openCallsUserSettings), keeps the classic sections.
 */
export function useFusionSettings(): [(el: HTMLElement | null) => void, boolean] {
    const [fusion, setFusion] = useState(false);
    const ref = useCallback((el: HTMLElement | null) => {
        if (el) {
            setFusion(isFusion() && Boolean(el.closest('.am-layer, .am-app')));
        }
    }, []);
    return [ref, fusion];
}

type SectionProps = {
    title: string;
    description: string;
    active: boolean;
    onEdit: () => void;
    onSave: () => void;
    onCancel: () => void;
    children?: React.ReactNode;
    sectionRef: (el: HTMLElement | null) => void;
};

/**
 * FusionSettingsSection is a Calls settings section as Fusion draws its settings: a row with the section's title,
 * what it's for and an Edit button; once edited, the section's heading, its fields (Fusion's .am-field) and Cancel
 * and Save.
 */
export function FusionSettingsSection({title, description, active, onEdit, onSave, onCancel, children, sectionRef}: SectionProps) {
    const {formatMessage} = useIntl();
    const headingId = useId();
    if (!active) {
        return (
            <div
                ref={sectionRef}
                className='am-toggle-row amcalls-set-row'
            >
                <div className='am-txt'>
                    <b>{title}</b>
                    <span>{description}</span>
                </div>
                <button
                    type='button'
                    className='am-btn'
                    aria-expanded={false}
                    onClick={onEdit}
                >
                    {formatMessage({defaultMessage: 'Edit'})}
                </button>
            </div>
        );
    }
    return (
        <section
            ref={sectionRef}
            className='amcalls-set'
            aria-labelledby={headingId}
        >
            <h3
                id={headingId}
                className='am-set-section'
            >{title}</h3>
            <p className='amcalls-set-lead'>{description}</p>
            {children}
            <div className='am-modal-actions'>
                <button
                    type='button'
                    className='am-btn'
                    onClick={onCancel}
                >
                    {formatMessage({defaultMessage: 'Cancel'})}
                </button>
                <button
                    type='button'
                    className='am-btn am-primary'
                    onClick={onSave}
                >
                    {formatMessage({defaultMessage: 'Save'})}
                </button>
            </div>
        </section>
    );
}

type SelectProps = {
    label: string;
    options: SelectOption[];
    value: SelectOption;
    onChange: (opt: SelectOption) => void;
};

// FusionSelect is a device list as Fusion's fields: its label above, the browser's select in Fusion's look.
export function FusionSelect({label, options, value, onChange}: SelectProps) {
    return (
        <label className='am-field'>
            <span>{label}</span>
            <select
                aria-label={label}
                value={value.value}
                disabled={options.length === 0}
                onChange={(e) => {
                    const opt = options.find((o) => o.value === e.target.value);
                    if (opt) {
                        onChange(opt);
                    }
                }}
            >
                {options.map((opt) => (
                    <option
                        key={opt.value}
                        value={opt.value}
                    >
                        {opt.label}
                    </option>
                ))}
            </select>
        </label>
    );
}

type SwitchProps = {
    title: string;
    text?: string;
    checked: boolean;
    onChange: (checked: boolean) => void;
    rowRef?: (el: HTMLElement | null) => void;
};

// FusionSwitchRow is an on/off setting as Fusion's: its name and what it does, and Fusion's switch.
export function FusionSwitchRow({title, text, checked, onChange, rowRef}: SwitchProps) {
    return (
        <div
            ref={rowRef}
            className='am-toggle-row'
        >
            <div className='am-txt'>
                <b>{title}</b>
                {text && <span>{text}</span>}
            </div>
            <button
                type='button'
                className='am-switch'
                role='switch'
                aria-checked={checked}
                aria-label={title}
                onClick={() => onChange(!checked)}
            />
        </div>
    );
}
