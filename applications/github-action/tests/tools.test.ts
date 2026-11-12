import assert from 'node:assert/strict';
import { test } from 'node:test';

import { copyFile, link, readFile } from 'node:fs/promises';
import { join } from 'node:path';

import { capture, temporary } from '#native';
import { executeNative } from '#tools';

async function withEnvironment(values: Record<string, string>, run: () => Promise<void>): Promise<void> {
	const previous = new Map(Object.keys(values).map((name) => [name, process.env[name]]));
	try {
		for (const [name, value] of Object.entries(values)) process.env[name] = value;
		await run();
	} finally {
		for (const [name, value] of previous) {
			if (value === undefined) delete process.env[name];
			else process.env[name] = value;
		}
	}
}

test('native spawn preserves executable paths, arguments and exit status', async () => {
	await temporary(async (directory) => {
		const name = process.platform === 'win32' ? 'node executable.exe' : 'node \\ " executable';
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
	});
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
			], { ACTIONLINT_TEST_LITERAL: 'a "b" \\ c', INPUT_TOKEN: 'fixture-review-token' });
			assert.equal(code, 3);
			const environment = JSON.parse(await readFile(output, 'utf8'));
			assert.equal(environment.ACTIONLINT_TEST_SENTINEL, undefined);
			assert.equal(environment.ACTIONLINT_TEST_LITERAL, 'a "b" \\ c');
			assert.equal(environment.INPUT_TOKEN, undefined);
			assert.equal(process.env.ACTIONLINT_TEST_SENTINEL, 'parent-only');
		});
	} finally {
		if (previous === undefined) delete process.env.ACTIONLINT_TEST_SENTINEL;
		else process.env.ACTIONLINT_TEST_SENTINEL = previous;
	}
});

test('tool probes exclude the review token from explicit and inherited environments', async () => {
	await withEnvironment({ INPUT_TOKEN: 'fixture-review-token' }, async () => {
		for (const environment of [undefined, { INPUT_TOKEN: 'fixture-review-token' }]) {
			const result = await capture(process.execPath, [
				'-e',
				'process.stdout.write(process.env.INPUT_TOKEN === undefined ? "absent" : "present")',
			], environment);
			assert.equal(result.exitCode, 0, result.stderr);
			assert.equal(result.stdout, 'absent');
		}
	});
});
