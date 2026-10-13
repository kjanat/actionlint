import assert from 'node:assert/strict';
import { test } from 'node:test';

import { fetchActionCatalogue } from './catalogue.ts';

void test('catalogue fetch preserves bytes for the WASM digest check', async () => {
	const source = '{"version":1}\n';
	const actual = await fetchActionCatalogue('catalogue.json', () => Promise.resolve(new Response(source)));
	assert.equal(actual, source);
});

void test('catalogue fetch rejects HTTP and network failures', async () => {
	await assert.rejects(
		fetchActionCatalogue('missing.json', () => Promise.resolve(new Response('not found', { status: 404 }))),
		/HTTP 404/,
	);
	await assert.rejects(
		fetchActionCatalogue('offline.json', () => Promise.reject(new Error('offline'))),
		/offline/,
	);
});
