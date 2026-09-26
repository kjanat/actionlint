import assert from 'node:assert/strict';
import test from 'node:test';

import { spawnSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
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

test('published smoke checks both external refs with inline Node', async (t) => {
	const action = (await readFile(new URL('./testdata/published-action/action.yml', import.meta.url), 'utf8'))
		.replaceAll('\r\n', '\n');
	assert.match(action, /uses: OWNER\/REPO@RELEASE_TAG/);
	assert.match(action, /uses: OWNER\/REPO@COMMIT_SHA/);
	assert.equal(action.match(/ACTIONLINT_ACTION_BINARY: ""/g)?.length, 2);
	assert.match(action, /uses: kjanat\/actions-shells@[a-f0-9]{40}/);
	assert.match(action, /shell: actions-shell node \{0\}/);
	assert.ok(action.includes('VERSION_OUTPUTS: ${{ toJSON(steps.version.outputs) }}'));
	assert.ok(action.includes('COMMIT_OUTPUTS: ${{ toJSON(steps.commit.outputs) }}'));
	const blocks = action.split('      run: |\n');
	assert.equal(blocks.length, 2);
	const directory = await mkdtemp(join(tmpdir(), 'actionlint inline node '));
	t.after(() => rm(directory, { recursive: true, force: true }));
	const inline = join(directory, 'runner-script');
	await writeFile(inline, blocks[1].replace(/^ {8}/gm, ''));
	const outputs = { 'exit-code': '0', result: 'success', 'problem-count': '0', output: JSON.stringify(clean) };
	for (const failing of ['', 'VERSION_OUTPUTS', 'COMMIT_OUTPUTS']) {
		/** @type {NodeJS.ProcessEnv} */
		const env = { ...process.env, VERSION_OUTPUTS: JSON.stringify(outputs), COMMIT_OUTPUTS: JSON.stringify(outputs) };
		if (failing) env[failing] = JSON.stringify({ ...outputs, output: JSON.stringify({ ...clean, diagnostics: [{}] }) });
		const result = spawnSync(process.execPath, [inline], { encoding: 'utf8', timeout: 5_000, env });
		assert.ifError(result.error);
		if (failing) assert.notEqual(result.status, 0, failing);
		else assert.equal(result.status, 0, result.stderr);
	}
});
