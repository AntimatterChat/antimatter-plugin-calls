// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// While a call is transcribed by a speech-to-text API (TranscribeFromClients), each participant's client sends what
// its own microphone hears: a voice activity detector cuts the audio into utterances, at a pause or when one grows
// long, and each is sent to the server as a WAV file to be transcribed (and shown as live captions). Only speech is
// sent, and nothing while muted: a muted track gives silence.

const TARGET_RATE = 16000;

// How the detector listens, in milliseconds.
const PRE_ROLL_MS = 300; // kept before the speech starts, so its first sound isn't lost
const END_SILENCE_MS = 700; // a pause this long ends an utterance
const SOFT_MAX_MS = 6000; // past this, a shorter pause ends it too
const SOFT_SILENCE_MS = 250;
const HARD_MAX_MS = 12000; // and it's cut here whatever happens
const MIN_SPEECH_MS = 300; // shorter utterances are noise

export type UtteranceSender = (audio: Blob, startAt: number, sentAt: number) => Promise<void>;

export default class UtteranceTranscriber {
    private ctx: AudioContext | null = null;
    private source: MediaStreamAudioSourceNode | null = null;
    private processor: ScriptProcessorNode | null = null;
    private track: MediaStreamTrack | null = null;

    private preRoll: Float32Array[] = [];
    private preRollMs = 0;
    private frames: Float32Array[] = [];
    private utteranceMs = 0;
    private speechMs = 0;
    private silenceMs = 0;
    private startAt = 0;
    private noiseFloor = 0.003;

    // Utterances are sent one after the other, in order.
    private queue: Promise<void> = Promise.resolve();
    private send: UtteranceSender;

    constructor(send: UtteranceSender) {
        this.send = send;
    }

    // attach listens to a microphone track, replacing the previous one (e.g. after a device change).
    attach(track: MediaStreamTrack | null) {
        if (track === this.track) {
            return;
        }
        this.flush();
        this.teardown();
        this.track = track;
        if (!track) {
            return;
        }

        const ctx = new AudioContext();
        this.ctx = ctx;
        this.source = ctx.createMediaStreamSource(new MediaStream([track]));

        // ScriptProcessorNode is deprecated but runs everywhere without a worklet module to serve.
        this.processor = ctx.createScriptProcessor(4096, 1, 1);
        this.processor.onaudioprocess = (ev) => this.process(ev.inputBuffer.getChannelData(0), ctx.sampleRate);
        this.source.connect(this.processor);
        this.processor.connect(ctx.destination);
        ctx.resume().catch(() => null);
    }

    // stop sends the utterance in progress and stops listening.
    stop() {
        this.flush();
        this.teardown();
        this.track = null;
    }

    private teardown() {
        this.processor?.disconnect();
        this.source?.disconnect();
        this.ctx?.close().catch(() => null);
        this.processor = null;
        this.source = null;
        this.ctx = null;
        this.preRoll = [];
        this.preRollMs = 0;
    }

    private process(input: Float32Array, rate: number) {
        const frame = new Float32Array(input);
        const ms = (frame.length / rate) * 1000;

        let sum = 0;
        for (let i = 0; i < frame.length; i++) {
            sum += frame[i] * frame[i];
        }
        const rms = Math.sqrt(sum / frame.length);

        // The noise floor follows the quiet moments, slowly; speech is well above it.
        const speaking = rms > Math.max(0.012, this.noiseFloor * 3.5);
        if (!speaking) {
            this.noiseFloor = (this.noiseFloor * 0.95) + (rms * 0.05);
        }

        if (!this.frames.length) {
            if (speaking) {
                this.startAt = Math.round(Date.now() - this.preRollMs - ms);
                this.frames = [...this.preRoll, frame];
                this.utteranceMs = this.preRollMs + ms;
                this.speechMs = ms;
                this.silenceMs = 0;
                this.preRoll = [];
                this.preRollMs = 0;
            } else {
                this.preRoll.push(frame);
                this.preRollMs += ms;
                while (this.preRollMs > PRE_ROLL_MS && this.preRoll.length > 1) {
                    this.preRollMs -= (this.preRoll.shift()!.length / rate) * 1000;
                }
            }
            return;
        }

        this.frames.push(frame);
        this.utteranceMs += ms;
        if (speaking) {
            this.speechMs += ms;
            this.silenceMs = 0;
        } else {
            this.silenceMs += ms;
        }

        if (this.silenceMs >= END_SILENCE_MS ||
            (this.utteranceMs >= SOFT_MAX_MS && this.silenceMs >= SOFT_SILENCE_MS) ||
            this.utteranceMs >= HARD_MAX_MS) {
            this.flush(rate);
        }
    }

    // flush sends the utterance in progress, if it has speech enough.
    private flush(rate = this.ctx?.sampleRate || 48000) {
        const frames = this.frames;
        const speechMs = this.speechMs;
        const startAt = this.startAt;
        this.frames = [];
        this.utteranceMs = 0;
        this.speechMs = 0;
        this.silenceMs = 0;
        if (!frames.length || speechMs < MIN_SPEECH_MS) {
            return;
        }

        const wav = encodeWAV(downsample(frames, rate, TARGET_RATE), TARGET_RATE);
        const sentAt = Date.now();
        this.queue = this.queue.then(() => this.send(wav, startAt, sentAt)).catch(() => undefined);
    }
}

// downsample joins frames and resamples them to the target rate (averaging, which filters what would alias).
export function downsample(frames: Float32Array[], rate: number, target: number): Float32Array {
    const length = frames.reduce((n, f) => n + f.length, 0);
    const joined = new Float32Array(length);
    let offset = 0;
    for (const f of frames) {
        joined.set(f, offset);
        offset += f.length;
    }
    if (rate === target) {
        return joined;
    }
    const ratio = rate / target;
    const out = new Float32Array(Math.floor(length / ratio));
    for (let i = 0; i < out.length; i++) {
        const start = Math.floor(i * ratio);
        const end = Math.min(length, Math.floor((i + 1) * ratio));
        let sum = 0;
        for (let j = start; j < end; j++) {
            sum += joined[j];
        }
        out[i] = sum / Math.max(1, end - start);
    }
    return out;
}

// encodeWAV encodes mono samples as 16-bit PCM WAV.
export function encodeWAV(samples: Float32Array, rate: number): Blob {
    const buffer = new ArrayBuffer(44 + (samples.length * 2));
    const view = new DataView(buffer);
    const text = (offset: number, s: string) => {
        for (let i = 0; i < s.length; i++) {
            view.setUint8(offset + i, s.charCodeAt(i));
        }
    };
    text(0, 'RIFF');
    view.setUint32(4, 36 + (samples.length * 2), true);
    text(8, 'WAVE');
    text(12, 'fmt ');
    view.setUint32(16, 16, true);
    view.setUint16(20, 1, true); // PCM
    view.setUint16(22, 1, true); // mono
    view.setUint32(24, rate, true);
    view.setUint32(28, rate * 2, true);
    view.setUint16(32, 2, true);
    view.setUint16(34, 16, true);
    text(36, 'data');
    view.setUint32(40, samples.length * 2, true);
    for (let i = 0; i < samples.length; i++) {
        const s = Math.max(-1, Math.min(1, samples[i]));
        view.setInt16(44 + (i * 2), s < 0 ? s * 0x8000 : s * 0x7fff, true);
    }
    return new Blob([buffer], {type: 'audio/wav'});
}
