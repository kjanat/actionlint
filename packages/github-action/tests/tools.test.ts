import assert from 'node:assert/strict';
import { test } from 'node:test';

import { copyFile, link, mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { capture, temporary } from '#native';
import { executeNative } from '#tools';

test('native spawn preserves executable paths, arguments and exit status', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint exec-'));
	try {
		const name = process.platform === 'win32' ? 'node.exe' : 'node \\ " executable';
		const executable = join(directory, name);
		try {
			await link(process.execPath, executable);
		} catch {
			await copyFile(process.execPath, executable);
		}
		const args = ['a b', 'a"b', 'a\\b', '$(not-a-command)', '%PATH%', ''];
		const result = await capture(executable, [
			'-e',
			'process.stdout.write(JSON.stringify(process.argv.slice(1))); process.exitCode = 7;',
			'--',
			...args,
		]);
		assert.equal(result.exitCode, 7);
		assert.deepEqual(JSON.parse(result.stdout), args);
		assert.equal(result.stderr, '');
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('action execution uses only the supplied child environment and preserves exit codes', async () => {
	const previous = process.env.ACTIONLINT_TEST_SENTINEL;
	process.env.ACTIONLINT_TEST_SENTINEL = 'parent-only';
	try {
		await temporary(async (directory) => {
			const output = join(directory, 'environment.json');
			const code = await executeNative(process.execPath, [
				'-e',
				'require("node:fs").writeFileSync(process.argv[1], JSON.stringify(process.env)); process.exitCode = 3;',
				output,
			], { ACTIONLINT_TEST_LITERAL: 'a "b" \\ c' });
			assert.equal(code, 3);
			const environment = JSON.parse(await readFile(output, 'utf8'));
			assert.equal(environment.ACTIONLINT_TEST_SENTINEL, undefined);
			assert.equal(environment.ACTIONLINT_TEST_LITERAL, 'a "b" \\ c');
			assert.equal(process.env.ACTIONLINT_TEST_SENTINEL, 'parent-only');
		});
	} finally {
		if (previous === undefined) delete process.env.ACTIONLINT_TEST_SENTINEL;
		else process.env.ACTIONLINT_TEST_SENTINEL = previous;
	}
});
