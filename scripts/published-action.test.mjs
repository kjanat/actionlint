import assert from 'node:assert/strict';
import test from 'node:test';

import { spawnSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('./check-action-outputs.mjs', import.meta.url));
const clean = {
	schema_version: 1,
	status: 'success',
	completed: true,
	exit_code: 0,
	file_count: 1,
	diagnostics: [],
	configurations: [],
	hints: [],
};

test('output checks accept clean results and additive fields', () => {
	for (const output of [clean, { ...clean, future_field: { enabled: true } }]) {
		const result = spawnSync(process.execPath, [script], {
			encoding: 'utf8',
			timeout: 5_000,
			env: { ...process.env, EXIT_CODE: '0', RESULT: 'success', PROBLEM_COUNT: '0', OUTPUT: JSON.stringify(output) },
		});
		assert.ifError(result.error);
		assert.equal(result.status, 0, result.stderr);
	}
});

test('output checks reject failures, findings, and invalid reports', () => {
	for (
		const invalid of [
			{ EXIT_CODE: '1' },
			{ RESULT: 'failure' },
			{ PROBLEM_COUNT: '1' },
			{ OUTPUT: '' },
			{ OUTPUT: '{' },
			{ OUTPUT: '[]' },
			{ OUTPUT: JSON.stringify({ ...clean, completed: false }) },
			{ OUTPUT: JSON.stringify({ ...clean, status: 'failure', exit_code: 3 }) },
			{ OUTPUT: JSON.stringify({ ...clean, diagnostics: [{ rule: 'shellcheck' }] }) },
		]
	) {
		/** @type {import('node:child_process').SpawnSyncReturns<string>} */
		const result = spawnSync(process.execPath, [script], {
			encoding: 'utf8',
			timeout: 5_000,
			env: {
				...process.env,
				EXIT_CODE: '0',
				RESULT: 'success',
				PROBLEM_COUNT: '0',
				OUTPUT: JSON.stringify(clean),
				...invalid,
			},
		});
		assert.ifError(result.error);
		assert.notEqual(result.status, 0, JSON.stringify(invalid));
	}
});

test('published smoke keeps both external refs and downloads their binaries', async () => {
	const action = await readFile(new URL('./testdata/published-action/action.yml', import.meta.url), 'utf8');
	assert.match(action, /uses: OWNER\/REPO@RELEASE_TAG/);
	assert.match(action, /uses: OWNER\/REPO@COMMIT_SHA/);
	assert.equal(action.match(/ACTIONLINT_ACTION_BINARY: ""/g)?.length, 2);
	assert.equal(action.match(/run: node scripts\/check-action-outputs\.mjs/g)?.length, 2);
	for (const id of ['version', 'commit']) {
		for (const output of ['exit-code', 'result', 'problem-count', 'output']) {
			assert.ok(action.includes(`\${{ steps.${id}.outputs.${output} }}`));
		}
	}
});
