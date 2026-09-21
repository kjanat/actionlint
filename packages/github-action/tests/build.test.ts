import assert from 'node:assert/strict';
import { test } from 'node:test';

import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const packageDirectory = fileURLToPath(new URL('..', import.meta.url));
const repositoryDirectory = resolve(packageDirectory, '../..');
const builder = fileURLToPath(import.meta.resolve('tsdown/run'));

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
		assert.match(result.stderr, /Refusing to create dist inside the source checkout/);
		assert.equal(existsSync(output), false);
	}
});
