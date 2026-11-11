import assert from 'node:assert/strict';
import test from 'node:test';

import { chmod, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { compareVersions, parseVersion, readVersion, writeUpdates } from '#release/version-file.mjs';

/** @param {import('node:test').TestContext} t */
async function fixture(t) {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-version-'));
	t.after(() => rm(root, { recursive: true, force: true }));
	await writeFile(join(root, 'VERSION'), '1.2.0\n');
	await writeFile(join(root, 'CHANGELOG.md'), 'Pending\n');
	return {
		root,
		updates: [
			{ path: 'VERSION', original: '1.2.0\n', content: '2.0.0\n' },
			{ path: 'CHANGELOG.md', original: 'Pending\n', content: 'Released\n' },
		],
	};
}

test('release versions have numeric components without prefixes, suffixes or leading zeroes', () => {
	for (const version of ['1.2.3', '0.0.0', '1.11.0']) {
		assert.equal(parseVersion(version).map(String).join('.'), version);
	}
	for (const version of ['v1.2.3', '01.2.3', '1.02.3', '1.2.03', '1.2', '1.2.3.4', '1.2.3-rc1', '']) {
		assert.throws(() => parseVersion(version), /three dot-separated/);
	}
	assert.equal(compareVersions('1.2.3', '1.2.3'), 0);
	assert.equal(compareVersions('1.2.3', '1.2.4'), -1);
	assert.equal(compareVersions('1.2.4', '1.2.3'), 1);
	assert.equal(compareVersions('1.9.0', '1.10.0'), -1);
	assert.equal(compareVersions('2.0.0', '1.99.99'), 1);
});

test('VERSION holds one release version, with or without surrounding whitespace', async t => {
	const { root } = await fixture(t);
	assert.equal(await readVersion(root), '1.2.0');
	await writeFile(join(root, 'VERSION'), '1.2.1\r\n');
	assert.equal(await readVersion(root), '1.2.1');
	await writeFile(join(root, 'VERSION'), 'v1.2.1\n');
	await assert.rejects(readVersion(root), /three dot-separated/);
	await rm(join(root, 'VERSION'));
	await assert.rejects(readVersion(root), { code: 'ENOENT' });
});

test('a write batch replaces every file and a repeated batch changes nothing', async t => {
	const { root, updates } = await fixture(t);
	await writeUpdates(root, updates);
	assert.equal(await readVersion(root), '2.0.0');
	assert.equal(await readFile(join(root, 'CHANGELOG.md'), 'utf8'), 'Released\n');
	await writeUpdates(root, updates.map(update => ({ ...update, original: update.content })));
	assert.equal(await readFile(join(root, 'VERSION'), 'utf8'), '2.0.0\n');
	assert.ok((await readdir(root)).every(name => !name.includes('.version-')));
});

test('read-only targets cause completed writes to roll back without temporary files', async t => {
	const { root, updates } = await fixture(t);
	const locked = join(root, 'CHANGELOG.md');
	await chmod(locked, 0o444);
	try {
		await assert.rejects(writeUpdates(root, updates), /read-only/);
		for (const update of updates) assert.equal(await readFile(join(root, update.path), 'utf8'), update.original);
		assert.ok((await readdir(root)).every(name => !name.includes('.version-')));
	} finally {
		await chmod(locked, 0o600);
	}
});

test('edits made after preparation are preserved and stop the write batch', async t => {
	const { root, updates } = await fixture(t);
	await writeFile(join(root, 'CHANGELOG.md'), 'User edit');
	await assert.rejects(writeUpdates(root, updates), /changed after version preparation/);
	assert.equal(await readFile(join(root, 'VERSION'), 'utf8'), '1.2.0\n');
	assert.equal(await readFile(join(root, 'CHANGELOG.md'), 'utf8'), 'User edit');
});

test('the repository VERSION is a release version', async () => {
	assert.match(await readVersion(resolve(import.meta.dirname, '../..')), /^\d+\.\d+\.\d+$/);
});
