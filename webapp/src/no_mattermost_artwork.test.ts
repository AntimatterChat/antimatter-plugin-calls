// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// Calls draws no Mattermost illustration (the laptop of the insecure context error was the last one): its dialogs
// are Fusion's, with Fusion's icons, and the classic ones keep their text. This scans what the bundle is built from.

import fs from 'fs';
import path from 'path';

const SRC = __dirname;

const OLD_ARTWORK = ['components/icons/laptop_alert_svg'];

const sourceFiles = (dir: string): string[] => fs.readdirSync(dir, {withFileTypes: true}).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
        return entry.name === 'node_modules' ? [] : sourceFiles(full);
    }
    return [full];
});

const files = sourceFiles(SRC).filter((f) => f !== __filename);
const code = files.filter((f) => (/\.(tsx?|jsx?|s?css)$/).test(f));

// The modules a file imports (import ... from, import '...', require(), import(), @import, url()).
const importsOf = (file: string): string[] => {
    const text = fs.readFileSync(file, 'utf8');
    return [
        ...text.matchAll(/(?:from\s+|import\s+|require\(\s*|import\(\s*)['"]([^'"]+)['"]/g),
        ...text.matchAll(/@import\s+['"]([^'"]+)['"]/g),
        ...text.matchAll(/url\(\s*['"]?([^'")]+)['"]?\s*\)/g),
    ].map((m) => m[1]);
};

describe('Mattermost artwork', () => {
    test('the old illustrations are gone from the tree', () => {
        const left = OLD_ARTWORK.filter((spec) => ['.tsx', '.ts', '.svg', '.png'].some((ext) => fs.existsSync(path.join(SRC, spec + ext))));
        expect(left).toEqual([]);
    });

    test('no module imports them, nor any image', () => {
        const offenders = code.flatMap((file) => importsOf(file).
            filter((spec) => OLD_ARTWORK.some((art) => spec.endsWith(art) || spec.endsWith(path.basename(art))) || (/\.(png|jpe?g|gif|webp|svg)$/).test(spec)).
            map((spec) => `${path.relative(SRC, file)} imports ${spec}`));
        expect(offenders).toEqual([]);
    });

    test('no image file ships in the source', () => {
        expect(files.filter((f) => (/\.(png|jpe?g|gif|webp|svg)$/).test(f)).map((f) => path.relative(SRC, f))).toEqual([]);
    });

    test('no component draws an illustration inline: its SVGs are icons, 48 units at most', () => {
        const drawings = code.filter((f) => (/\.tsx$/).test(f)).flatMap((file) => {
            const text = fs.readFileSync(file, 'utf8');
            return [...text.matchAll(/viewBox=['"{`]+\s*([\d.-]+)[\s,]+([\d.-]+)[\s,]+([\d.]+)[\s,]+([\d.]+)/g)].
                filter((m) => Number(m[3]) > 48 || Number(m[4]) > 48).
                map((m) => `${path.relative(SRC, file)}: viewBox ${m[1]} ${m[2]} ${m[3]} ${m[4]}`);
        });
        expect(drawings).toEqual([]);
    });

    test('the check finds an illustration', () => {
        const re = /viewBox=['"{`]+\s*([\d.-]+)[\s,]+([\d.-]+)[\s,]+([\d.]+)[\s,]+([\d.]+)/;
        const m = re.exec('viewBox=\'0 0 246 182\'')!;
        expect(Number(m[3])).toBe(246);
    });
});
