import assert from 'node:assert/strict';
import { test } from 'node:test';

import { copyFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';

import { normalizeEnvironment } from '#environment';
import { capture, temporary, which } from '#native';

test('Windows environment overrides replace case variants; Unix preserves distinct names', () => {
	const input = { Path: 'old', PATH: 'new', input_config: 'value', missing: undefined };
	assert.deepEqual(normalizeEnvironment(input, 'win32'), { PATH: 'new', INPUT_CONFIG: 'value' });
	assert.deepEqual(normalizeEnvironment(input, 'linux'), { Path: 'old', PATH: 'new', input_config: 'value' });
	assert.equal(input.Path, 'old');
});

test('Windows PATH lookup and child execution honor the same mixed-case environment', {
	skip: process.platform !== 'win32',
}, async () => {
	await temporary(async (directory) => {
		const tools = join(directory, 'tools with spaces');
		await mkdir(tools);
		const executable = join(tools, 'probe.exe');
		await copyFile(process.execPath, executable);
		const environment = { Path: tools, ACTIONLINT_TEST_VALUE: 'stale', actionlint_test_value: 'selected' };
		assert.equal(await which('probe', environment), executable);
		const result = await capture(
			'probe',
			['-e', 'process.stdout.write(process.env.ACTIONLINT_TEST_VALUE)'],
			environment,
		);
		assert.equal(result.exitCode, 0, result.stderr);
		assert.equal(result.stdout, 'selected');
	});
});
