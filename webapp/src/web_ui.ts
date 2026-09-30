// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// Antimatter ships two web UIs: the classic one and Fusion. Fusion draws the call UI itself and
// drives Calls through window.antimatterCalls, so Calls leaves its own call UI out under it.
// Both UIs say which one is running before plugins load.

export type WebUI = 'classic' | 'fusion';

// isFusionUI returns whether the Fusion web UI is running.
export function isFusionUI(): boolean {
    return window.antimatterWebUI === 'fusion' || document.documentElement.dataset.amWebUi === 'fusion';
}
