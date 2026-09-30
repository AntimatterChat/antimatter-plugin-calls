// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {isFusionUI} from './web_ui';

describe('isFusionUI', () => {
    afterEach(() => {
        delete window.antimatterWebUI;
        delete document.documentElement.dataset.amWebUi;
    });

    test('false when no web UI is announced', () => {
        expect(isFusionUI()).toBe(false);
    });

    test('false under the classic web UI', () => {
        window.antimatterWebUI = 'classic';
        document.documentElement.dataset.amWebUi = 'classic';
        expect(isFusionUI()).toBe(false);
    });

    test('true when the global says fusion', () => {
        window.antimatterWebUI = 'fusion';
        expect(isFusionUI()).toBe(true);
    });

    test('true when the html attribute says fusion', () => {
        document.documentElement.dataset.amWebUi = 'fusion';
        expect(isFusionUI()).toBe(true);
    });
});
