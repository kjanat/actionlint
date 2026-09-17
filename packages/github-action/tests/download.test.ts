import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createHash } from 'node:crypto';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { download, downloadVerified } from '#download';

test('downloaded archives are accepted only after checksum verification', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-download-test-'));
	try {
		const destination = join(directory, 'archive.zip');
		const body = 'verified archive bytes';
		const download = async (_url: string, path: string): Promise<string> => {
			await writeFile(path, body);
			return path;
		};
		const digest = createHash('sha256').update(body).digest('hex');
		const downloaded = await downloadVerified(download, 'https://example.invalid/archive.zip', destination, digest);
		assert.equal(await readFile(downloaded, 'utf8'), body);
		await assert.rejects(
			downloadVerified(download, 'https://example.invalid/archive.zip', destination, '0'.repeat(64)),
			/SHA-256 checksum mismatch/,
		);
		await assert.rejects(readFile(destination), { code: 'ENOENT' });
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('native download rejects HTTP failures and preserves existing files', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-http-test-'));
	try {
		const destination = join(directory, 'archive');
		await assert.rejects(
			download('https://example.invalid/archive', destination, async () => new Response('missing', { status: 404 })),
			/HTTP 404/,
		);
		await assert.rejects(readFile(destination), { code: 'ENOENT' });
		await download('https://example.invalid/archive', destination, async () => new Response('verified bytes'));
		assert.equal(await readFile(destination, 'utf8'), 'verified bytes');
		await assert.rejects(
			download('https://example.invalid/archive', destination, async () => new Response('replacement')),
			{ code: 'EEXIST' },
		);
		assert.equal(await readFile(destination, 'utf8'), 'verified bytes');
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});
