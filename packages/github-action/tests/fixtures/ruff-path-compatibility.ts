import assert from 'node:assert/strict';
import { mock } from 'node:test';

import { copyFile, mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { ruffVersion, runnerPlatform } from '../../src/assets.ts';
import * as native from '../../src/native.ts';

let banner = '';
let exitCode = 0;
let failure: Error | undefined;
mock.module(new URL('../../src/native.ts', import.meta.url).href, {
	namedExports: {
		...native,
		which: async () => process.execPath,
		capture: async () => {
			if (failure) throw failure;
			return { exitCode, stdout: banner, stderr: '' };
		},
	},
});
const { ruffBinary } = await import('../../src/tools.ts');
const platform = runnerPlatform(process.platform, process.arch);
const cache = process.env.RUNNER_TOOL_CACHE;
assert.ok(cache);
const root = join(cache, `actionlint-ruff-${platform.os}`, ruffVersion, platform.arch);
await mkdir(root, { recursive: true });
await writeFile(`${root}.complete`, '');
const executable = join(root, platform.os === 'windows' ? 'ruff.exe' : 'ruff');
await copyFile(process.execPath, executable);

for (
	const [version, compatible] of [
		['0.11.0', false],
		['0.16.999', false],
		['0.17.0-rc.1', false],
		['0.17.0', true],
		['0.17.0+build.1', true],
		['0.17.1', true],
		['0.18.0-rc.1', true],
		['0.100.0', true],
		['1.0.0', true],
	] as const
) {
	banner = `ruff ${version}\n`;
	assert.deepEqual(
		await ruffBinary(platform),
		compatible
			? { kind: 'existing', executable: process.execPath }
			: { kind: 'standalone', executable },
		version,
	);
}
for (const output of ['', 'custom wrapper', 'v26.10.0', 'ruff 0.17', 'ruff 00.17.0']) {
	banner = output;
	assert.deepEqual(await ruffBinary(platform), { kind: 'standalone', executable }, output);
}
banner = 'ruff 0.17.0';
exitCode = 2;
assert.deepEqual(await ruffBinary(platform), { kind: 'standalone', executable });
exitCode = 0;
failure = new Error('timed out after 1000ms');
assert.deepEqual(await ruffBinary(platform), { kind: 'standalone', executable });
