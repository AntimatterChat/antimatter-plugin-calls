// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {downsample, encodeWAV} from 'src/utterance_transcriber';

describe('utterance_transcriber', () => {
    test('downsample averages to the target rate', () => {
        const out = downsample([new Float32Array([0.3, 0.3, 0.3, 0.6]), new Float32Array([0.6, 0.6])], 48000, 16000);
        expect(Array.from(out).map((v) => Math.round(v * 10) / 10)).toEqual([0.3, 0.6]);
    });

    test('downsample keeps samples at the same rate', () => {
        expect(Array.from(downsample([new Float32Array([0.5]), new Float32Array([-0.5])], 16000, 16000))).toEqual([0.5, -0.5]);
    });

    test('encodeWAV writes a 16-bit mono PCM WAV', async () => {
        const blob = encodeWAV(new Float32Array([0, 1, -1, 2]), 16000);
        expect(blob.type).toBe('audio/wav');
        const buffer = await new Promise<ArrayBuffer>((resolve) => {
            const reader = new FileReader();
            reader.onload = () => resolve(reader.result as ArrayBuffer);
            reader.readAsArrayBuffer(blob);
        });
        const view = new DataView(buffer);
        const text = (offset: number) => String.fromCharCode(...[0, 1, 2, 3].map((i) => view.getUint8(offset + i)));
        expect(text(0)).toBe('RIFF');
        expect(text(8)).toBe('WAVE');
        expect(view.getUint16(22, true)).toBe(1);
        expect(view.getUint32(24, true)).toBe(16000);
        expect(view.getUint32(40, true)).toBe(8);
        expect([0, 1, 2, 3].map((i) => view.getInt16(44 + (i * 2), true))).toEqual([0, 32767, -32768, 32767]);
    });
});
