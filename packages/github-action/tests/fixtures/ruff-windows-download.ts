import assert from 'node:assert/strict';
import { mock } from 'node:test';

import { createHash } from 'node:crypto';
import { readdir, readFile, writeFile } from 'node:fs/promises';
import { basename, dirname } from 'node:path';

import * as assets from '../../src/assets.ts';
import * as download from '../../src/download.ts';
import * as native from '../../src/native.ts';

// Both pinned Windows release ZIPs contain one root-level ruff.exe.
const archive = Buffer.from(
	'UEsDBBQAAAAAAAAAAAAAM6DSHAAAABwAAAAIAAAAcnVmZi5leGVSdWZmIGFyY2hpdmUgbGF5b3V0IGZpeHR1cmUKUEsBAhQDFAAAAAAAAAAAAAAzoNIcAAAAHAAAAAgAAAAAAAAAAAAAAO2BAAAAAHJ1ZmYuZXhlUEsFBgAAAAABAAEANgAAAEIAAAAAAA==',
	'base64',
);
const digest = createHash('sha256').update(archive).digest('hex');
const downloads: string[] = [];
mock.module(new URL('../../src/assets.ts', import.meta.url).href, {
	namedExports: {
		...assets,
		ruffAsset: (platform: assets.RunnerPlatform) => ({ ...assets.ruffAsset(platform), sha256: digest }),
	},
});
mock.module(new URL('../../src/download.ts', import.meta.url).href, {
	namedExports: {
		...download,
		download: async (url: string, destination: string) => {
			downloads.push(url);
			await writeFile(destination, archive);
			return destination;
		},
	},
});
mock.module(new URL('../../src/native.ts', import.meta.url).href, {
	namedExports: { ...native, which: async () => '' },
});
const { ruffBinary } = await import('../../src/tools.ts');
for (const arch of ['amd64', 'arm64'] as const) {
	const platform: assets.RunnerPlatform = { os: 'windows', arch };
	const installed = await ruffBinary(platform);
	assert.equal(installed.kind, 'standalone');
	assert.equal(basename(installed.executable), 'ruff.exe');
	assert.deepEqual(await readdir(dirname(installed.executable)), ['ruff.exe']);
	assert.equal(await readFile(installed.executable, 'utf8'), 'Ruff archive layout fixture\n');
	assert.deepEqual(await ruffBinary(platform), installed);
	const target = arch === 'amd64' ? 'x86_64' : 'aarch64';
	assert.equal(
		downloads.at(-1),
		`https://github.com/astral-sh/ruff/releases/download/0.17.0/ruff-${target}-pc-windows-msvc.zip`,
	);
}
assert.equal(downloads.length, 2);
