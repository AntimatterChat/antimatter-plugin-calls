// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import fs from 'fs';
import path from 'path';

// Calls' Fusion stylesheet restyles its dialogs and settings on top of Fusion's classes: it must only ever apply
// under the Fusion UI (in Fusion's app or layer, which the classic UI doesn't have), never to the classic UI.
const SCOPE = ':is(.am-app, .am-layer) ';

const css = fs.readFileSync(path.join(__dirname, 'fusion.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');

// The selectors of the rules at the top level of the stylesheet, split at their commas (outside parentheses).
const selectors = () => css.split('}').map((rule) => rule.split('{')[0].trim()).filter(Boolean).flatMap((selector) => {
    const parts: string[] = [];
    let depth = 0;
    let start = 0;
    for (let i = 0; i < selector.length; i++) {
        if (selector[i] === '(') {
            depth++;
        } else if (selector[i] === ')') {
            depth--;
        } else if (selector[i] === ',' && depth === 0) {
            parts.push(selector.slice(start, i).trim());
            start = i + 1;
        }
    }
    parts.push(selector.slice(start).trim());
    return parts;
});

describe('Calls\' Fusion stylesheet', () => {
    test('has rules, none nested', () => {
        expect(selectors().length).toBeGreaterThan(0);
        expect(css).not.toMatch(/\{[^}]*\{/);
    });

    test('scopes every rule to Fusion\'s app or layer', () => {
        expect(selectors().filter((selector) => !selector.startsWith(SCOPE))).toEqual([]);
    });

    test('uses Fusion\'s tokens and fonts, not hard-coded colours or the classic theme', () => {
        expect(css.match(/#[0-9a-fA-F]{3,8}\b|rgba?\(/g) ?? []).toEqual([]);
        expect(css).not.toMatch(/--center-channel|--button-bg|--sidebar-|Metropolis|Open Sans/);
        expect(css).toContain('var(--am-');
    });

    test('is loaded with the Fusion dialogs and settings', () => {
        const read = (file: string) => fs.readFileSync(path.join(__dirname, '..', file), 'utf8');
        expect(read('fusion/fusion_dialog.tsx')).toContain('import \'./fusion.css\';');
        expect(read('user_settings/fusion_section.tsx')).toContain('import \'src/components/fusion/fusion.css\';');
    });
});
