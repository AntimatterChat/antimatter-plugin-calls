// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {isCurrentUserSystemAdmin} from 'mattermost-redux/selectors/entities/users';
import {useState} from 'react';
import {useSelector} from 'react-redux';

export const useHostControls = (isYou: boolean, isHost: boolean, iAmHost: boolean) => {
    const isAdmin = useSelector(isCurrentUserSystemAdmin);

    const [hover, setHover] = useState(false);
    const hoverOn = () => setHover(true);
    const hoverOff = () => setHover(false);

    const [sticky, setSticky] = useState(false);
    const onOpenChange = (open: boolean) => {
        setSticky(open);
        setHover(open);
    };

    const hostControlsAvailable = iAmHost || isAdmin;

    // Show host controls when available + hover, but don't show if this is me and I'm the host already,
    // When sticky is true, we always show.
    const showHostControls = (hostControlsAvailable && hover && !(isYou && isHost)) || sticky;

    return {
        hoverOn,
        hoverOff,
        onOpenChange,
        hostControlsAvailable,
        showHostControls,
    };
};
