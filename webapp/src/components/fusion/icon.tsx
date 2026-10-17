// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import CompassIcon from 'src/components/icons/compassIcon';

import {isFusion} from './fusion_ui';

// The icons Calls' dialogs draw from Fusion's line icon set (the sprite Fusion puts in the page, symbols
// #am-i-<name>), with the classic UI's Compass icon for each.
const CLASSIC_ICONS = {
    x: 'close',
    screen: 'monitor',
    speaker: 'volume-high',
} as const;

export type FusionIconName = keyof typeof CLASSIC_ICONS;

// The Fusion icons Calls uses: each must be a symbol of Fusion's sprite (see icon.test.tsx).
export const FUSION_ICON_NAMES = Object.keys(CLASSIC_ICONS) as FusionIconName[];

type Props = {
    name: FusionIconName;
    size?: 'sm' | 'xs';
    className?: string;
};

/**
 * FusionIcon draws one of Fusion's icons under the Fusion UI, and the matching Compass icon under the classic UI.
 * The stroke is set on the element too, so the icon looks right wherever it's drawn.
 */
export default function FusionIcon({name, size, className}: Props) {
    if (isFusion()) {
        const classes = ['am-ic', size ? `am-${size}` : '', className ?? ''].filter(Boolean).join(' ');
        return (
            <svg
                className={classes}
                fill='none'
                stroke='currentColor'
                strokeWidth={1.8}
                strokeLinecap='round'
                strokeLinejoin='round'
                aria-hidden='true'
                data-fusion-icon={name}
            >
                <use href={`#am-i-${name}`}/>
            </svg>
        );
    }
    return (
        <CompassIcon
            icon={CLASSIC_ICONS[name]}
            className={className ?? ''}
            aria-hidden='true'
        />
    );
}
