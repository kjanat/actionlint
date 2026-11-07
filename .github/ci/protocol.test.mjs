import assert from 'node:assert/strict';
import test from 'node:test';

import { readdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

import { readFilters, selectJobs } from './plan.mjs';

const root = resolve(import.meta.dirname, '../..');

/** @param {string} directory @param {string} prefix @returns {string[]} */
function files(directory, prefix = '') {
	return readdirSync(join(directory, prefix), { withFileTypes: true }).flatMap(entry => {
		const path = prefix ? `${prefix}/${entry.name}` : entry.name;
		if (entry.isDirectory()) return files(directory, path);
		assert.ok(entry.isFile(), `Expected a regular file: ${join(directory, path)}`);
		return [path];
	}).sort();
}

for (const kind of ['wit', 'json']) {
	test(`Rust SDK ${kind} files match the protocol`, () => {
		const source = join(root, 'plugins/protocol', kind);
		const copy = join(root, 'plugins/sdk/rust', kind);
		const paths = files(source);
		assert.deepEqual(files(copy), paths);
		for (const path of paths) {
			assert.deepEqual(readFileSync(join(copy, path)), readFileSync(join(source, path)), path);
		}
	});
}

test('Go SDK packaging analyzer WIT matches the protocol', () => {
	assert.deepEqual(
		readFileSync(join(root, 'plugins/sdk/go/packaging/analyzer.wit')),
		readFileSync(join(root, 'plugins/protocol/wit/analyzer.wit')),
	);
});

test('protocol and SDK copy changes select the synchronization tests', () => {
	const filters = readFilters('ci', root);
	for (
		const path of [
			'plugins/protocol/wit/analyzer.wit',
			'plugins/protocol/json/nested/schema.json',
			'plugins/sdk/rust/wit/analyzer.wit',
			'plugins/sdk/rust/json/nested/schema.json',
			'plugins/sdk/go/packaging/analyzer.wit',
		]
	) {
		assert.equal(selectJobs(filters, [path]).javascript, true, path);
	}
	assert.equal(
		selectJobs(filters, ['internal/development/tests/conformance/wasm/runner-host/src/main.rs']).javascript,
		false,
	);
});
