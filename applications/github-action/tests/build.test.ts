import assert from 'node:assert/strict';
import { test } from 'node:test';

import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { capture, temporary } from '#native';

const packageDirectory = fileURLToPath(new URL('..', import.meta.url));
const repositoryDirectory = resolve(packageDirectory, '../..');
const builder = fileURLToPath(import.meta.resolve('tsdown/run'));

test('release bundle includes the cache toolkit and starts without node_modules', async () => {
	await temporary(async (directory) => {
		const result = spawnSync(process.execPath, [builder, '--out-dir', directory], {
			cwd: packageDirectory,
			env: { ...process.env, ACTIONLINT_VERSION: '1.0.0' },
			encoding: 'utf8',
		});
		assert.equal(result.status, 0, result.stderr);
		const started = await capture(process.execPath, [join(directory, 'action.mjs')], {
			'INPUT_INSTALL-ONLY': 'true',
			'INPUT_ADD-ACTIONLINT-TO-PATH': 'false',
		});
		assert.equal(started.exitCode, 2, started.stderr);
		assert.match(started.stdout, /requires 'add-actionlint-to-path: true'/);
	});
});

test('bundle builder requires an explicit external output directory', () => {
	for (const args of [[], ['--out-dir', 'relative-output']]) {
		const result = spawnSync(process.execPath, [builder, ...args], { cwd: packageDirectory, encoding: 'utf8' });
		assert.equal(result.status, 1);
		assert.match(result.stderr, /absolute artifact directory outside the source checkout/);
	}
});

test('bundle builder refuses checkout descendants, including names beginning with two dots', () => {
	for (const name of ['action-build-must-not-exist', '..action-build-must-not-exist']) {
		const output = join(repositoryDirectory, name);
		const result = spawnSync(process.execPath, [builder, '--out-dir', output], {
			cwd: packageDirectory,
			encoding: 'utf8',
		});
		assert.equal(result.status, 1);
		assert.match(result.stderr, /Refusing to create the bundle inside the source checkout/);
		assert.equal(existsSync(output), false);
	}
});
