// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {useIntl} from 'react-intl';
import {useSelector} from 'react-redux';
import {rtcdEnabled} from 'src/selectors';
import {untranslatable} from 'src/utils';

export const useHelptext = (propsHelpText: string | JSX.Element | null) => {
    const {formatMessage} = useIntl();
    const isRTCDEnabled = useSelector(rtcdEnabled);

    if (!isRTCDEnabled) {
        return propsHelpText;
    }

    const space = untranslatable(' ');

    // @ts-ignore
    const disabledPostfix = formatMessage({defaultMessage: 'Not applicable when the <link>RTCD service URL</link> field is in use.'},
        {link: (text: React.ReactNode) => <a href={'https://antimatter.chat/docs/admins/calls.html#rtcd'}>{text}</a>});

    return (
        <>
            {propsHelpText}
            {space}
            {disabledPostfix}
        </>
    );
};
