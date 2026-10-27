import assert from 'node:assert/strict';
import test from 'node:test';

import { spawnSync } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const wrapper = fileURLToPath(new URL('./docker-action.sh', import.meta.url));
const inputs = [
	'FILES',
	'FORMAT',
	'IGNORE',
	'CONFIG-FILE',
	'WORKING-DIRECTORY',
	'OUTPUT-FILE',
	'FAIL-ON-ERROR',
];

test('Docker wrapper preserves seven literal arguments and native exit status', async () => {
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-docker-inputs-'));
	try {
		// Record env's literal assignments without a shell importing hyphenated names.
		await writeFile(join(temporary, 'env'), '#!/bin/sh\nprintf "%s\\n" "$@"\nexit 1\n', { mode: 0o755 });
		const values = [
			'first file.yaml\nsecond.yaml',
			'json',
			'quoted "pattern"',
			'config dir/lint.yaml',
			'sub directory',
			'report file.json',
			'false',
		];
		const result = spawnSync('bash', [
			'--noprofile',
			'--norc',
			'-c',
			'directory="$1"; if command -v cygpath >/dev/null; then directory="$(cygpath -u "$directory")"; fi; PATH="$directory:$PATH"; export PATH; shift; exec sh "$@"',
			'docker-wrapper-test',
			temporary,
			wrapper,
			...values,
		], {
			encoding: 'utf8',
			timeout: 5_000,
		});
		assert.ifError(result.error);
		assert.equal(result.status, 1, result.stdout + result.stderr);
		assert.ok(result.stdout.endsWith('actionlint\n-github-action\n'), result.stdout);
		for (const [index, name] of inputs.entries()) {
			assert.ok(result.stdout.includes(`INPUT_${name}=${values[index]}\n`), name);
		}
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
});

test('Docker wrapper rejects missing, extra, and empty required positional values', () => {
	const valid = ['', 'json', '', '', '.', '', 'true'];
	const cases = [
		[],
		valid.slice(0, 6),
		[...valid, 'extra'],
		...[1, 6].map((index) => valid.map((value, i) => i === index ? '' : value)),
	];
	for (const args of cases) {
		const result = spawnSync('bash', ['--noprofile', '--norc', wrapper, ...args], { encoding: 'utf8', timeout: 5_000 });
		assert.ifError(result.error);
		assert.equal(result.status, 2, result.stdout + result.stderr);
		assert.match(result.stdout, /^::error title=Invalid action input::/);
	}
});
