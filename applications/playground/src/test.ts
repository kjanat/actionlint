import { strict as assert } from 'node:assert';
import { describe, it } from 'vitest';

import * as pako from 'pako';

import {
	maxEncodedLength,
	maxSourceBytes,
	maxURLLength,
	querySource,
	readSource,
	sharedSource,
	shareURL,
} from '#source';

describe('source', () => {
	function encode(bytes: Uint8Array): string {
		return Buffer.from(bytes).toString('base64');
	}

	function shared(text: string): string {
		return encode(pako.deflate(new TextEncoder().encode(text)));
	}

	it('inflates a shared source', () => {
		assert.equal(sharedSource(shared('on: push\n')), 'on: push\n');
	});

	const base = 'https://kjanat.github.io/actionlint/';

	function hashOf(url: string): string {
		assert.ok(url.startsWith(`${base}#`), url.slice(0, 80));
		return url.slice(base.length + 1);
	}

	it('shares a source that the loader accepts', () => {
		assert.equal(sharedSource(hashOf(shareURL(base, 'on: push\n'))), 'on: push\n');
		const limit = 'a'.repeat(maxSourceBytes);
		assert.equal(sharedSource(hashOf(shareURL(base, limit))), limit);
	});

	it('refuses to share a source past the limit', () => {
		assert.throws(() => shareURL(base, 'a'.repeat(maxSourceBytes + 1)), /larger than/);
		assert.throws(() => shareURL(base, 'é'.repeat(maxSourceBytes / 2 + 1)), /larger than/);
	});

	it('refuses to share a URL past the browser limit', () => {
		const letters = 'abcdefghijklmnopqrstuvwxyz';
		const source = Array.from({ length: 2 * maxURLLength }, () => letters[Math.floor(Math.random() * 26)]).join('');
		assert.throws(() => shareURL(base, source), /longer than/);
	});

	it('bounds a query source', () => {
		assert.equal(querySource('on: push\n'), 'on: push\n');
		assert.throws(() => querySource('a'.repeat(maxSourceBytes + 1)), /larger than/);
	});

	// Exercise the full 16 MiB boundary, including compression, on slower runners.
	it('accepts an incompressible source at the limit', { timeout: 30_000 }, () => {
		const source = new Uint8Array(maxSourceBytes);
		for (let offset = 0; offset < source.length; offset += 65536) {
			crypto.getRandomValues(source.subarray(offset, offset + 65536));
		}
		const encoded = encode(pako.deflate(source));
		assert.ok(encoded.length <= maxEncodedLength, `${encoded.length} > ${maxEncodedLength}`);
		assert.equal(sharedSource(encoded).length > 0, true);
	});

	it('refuses an encoded source past the limit before decoding it', () => {
		assert.throws(() => sharedSource('A'.repeat(maxEncodedLength + 1)), /longer than/);
	});

	it('stops inflating past the source limit', () => {
		assert.throws(() => sharedSource(encode(pako.deflate(new Uint8Array(maxSourceBytes + 1)))), /inflates past/);
	});

	it('rejects a truncated stream', () => {
		const compressed = pako.deflate(new TextEncoder().encode('on: push\n'));
		assert.throws(
			() => sharedSource(encode(compressed.subarray(0, compressed.length - 4))),
			/not a complete zlib stream/,
		);
	});

	it('rejects data after the stream', () => {
		const compressed = pako.deflate(new TextEncoder().encode('on: push\n'));
		assert.throws(() => sharedSource(encode(new Uint8Array([...compressed, 0, 1, 2]))), /3 bytes after/);
	});

	it('reads a remote source', async () => {
		assert.equal(await readSource(new Response('on: push\n')), 'on: push\n');
		assert.equal(await readSource(new Response(null)), '');
	});

	it('stops reading past the source limit', async () => {
		await assert.rejects(readSource(new Response(new Uint8Array(maxSourceBytes + 1))), /larger than/);
	});
});
