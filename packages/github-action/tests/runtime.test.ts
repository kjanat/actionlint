import assert from 'node:assert/strict';
import { test } from 'node:test';

import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { Environment, Runtime } from '#runtime';
import { InputError, runAction, toolEnabled } from '#runtime';

function fixture(exitCode = 0) {
	const calls: string[] = [];
	const publications: string[] = [];
	const executions: { executable: string; args: string[]; environment: Environment }[] = [];
	const runtime: Runtime = {
		native: async () => {
			calls.push('native');
			return join(tmpdir(), 'downloaded actionlint');
		},
		checkExecutable: async () => {
			calls.push('check-executable');
		},
		plugins: async (_executable, _environment, run) => run(),
		publish: async (tools) => {
			publications.push(tools);
		},
		execute: async (executable, args, environment) => {
			executions.push({ executable, args, environment });
			return exitCode;
		},
	};
	return { calls, executions, publications, runtime };
}

test('plugin cache wraps Wasm hydration and execution, including findings', async () => {
	const setup = fixture(1);
	setup.runtime.plugins = async (_executable, _environment, run) => {
		setup.calls.push('restore');
		try {
			return await run();
		} finally {
			setup.calls.push('save');
		}
	};
	assert.equal(await runAction({}, setup.runtime), 1);
	assert.deepEqual(setup.calls, ['native', 'restore', 'save']);
	assert.equal(setup.executions.length, 1);
});

test('plugin cache can be disabled and install-only never inspects plugin configuration', async () => {
	for (const environment of [{ 'INPUT_PLUGIN-CACHE': 'false' }, { 'INPUT_INSTALL-ONLY': 'true' }]) {
		const setup = fixture();
		setup.runtime.plugins = async () => assert.fail('plugin cache must not be invoked');
		assert.equal(await runAction(environment, setup.runtime), 0);
	}
	const setup = fixture();
	await assert.rejects(runAction({ 'INPUT_PLUGIN-CACHE': 'sometimes' }, setup.runtime), InputError);
	assert.deepEqual(setup.calls, []);
});

test('deprecated Python input warns and cannot enable Pyflakes in a selected older binary', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	const setup = fixture();
	assert.equal(await runAction({ INPUT_PYFLAKES: 'true', INPUT_SHELLCHECK: 'false' }, setup.runtime), 0);
	assert.deepEqual(setup.calls, ['native']);
	assert.equal(setup.executions[0]?.environment.INPUT_PYFLAKES, 'false');
	assert.equal(setup.publications[0], join(tmpdir(), 'downloaded actionlint'));
	assert.ok(
		messages.some((message) =>
			message.startsWith('::warning::') && message.includes('pyflakes input is ignored')
			&& message.includes('Ruff Wasm plugin')
		),
	);
});

test('install-only publishes actionlint without plugin configuration or analysis', async () => {
	const setup = fixture();
	setup.runtime.plugins = async () => assert.fail('setup must not inspect workflows');
	setup.runtime.execute = async () => assert.fail('setup must not analyze');
	assert.equal(await runAction({ 'INPUT_INSTALL-ONLY': 'true', INPUT_CONFIG: 'not linted' }, setup.runtime), 0);
	assert.deepEqual(setup.calls, ['native']);
	assert.deepEqual(setup.publications, [join(tmpdir(), 'downloaded actionlint')]);
});

test('launcher validates setup inputs before downloading', async () => {
	for (
		const environment of [
			{ 'INPUT_INSTALL-ONLY': 'yes' },
			{ 'INPUT_ADD-ACTIONLINT-TO-PATH': 'yes' },
			{ 'INPUT_INSTALL-ONLY': 'true', 'INPUT_ADD-ACTIONLINT-TO-PATH': 'false' },
		]
	) {
		const setup = fixture();
		await assert.rejects(runAction(environment, setup.runtime), InputError);
		assert.deepEqual(setup.calls, []);
	}
});

test('ordinary binary receives explicit action mode, untouched inputs, and its exit code is preserved', async () => {
	const setup = fixture(1);
	const environment = {
		INPUT_SHELLCHECK: 'false',
		INPUT_PYFLAKES: 'false',
		INPUT_FILES: 'workflow name.yml\n$(not-a-command).yml',
		INPUT_CONFIG: '{"self-hosted-runner":{"labels":["ubuntu-24.04-custom"]}}',
		'INPUT_FAIL-ON-ERROR': 'false',
	};
	assert.equal(await runAction(environment, setup.runtime), 1);
	assert.equal(setup.calls.includes('shellcheck'), false);
	assert.equal(setup.calls.includes('pyflakes'), false);
	assert.deepEqual(setup.publications, [join(tmpdir(), 'downloaded actionlint')]);
	assert.deepEqual(setup.executions, [{
		executable: join(tmpdir(), 'downloaded actionlint'),
		args: ['-github-action'],
		environment: { ...environment, INPUT_RUFF: 'true' },
	}]);
});

test('plugin planning and analysis exclude the review token', async () => {
	const setup = fixture();
	const environment = { INPUT_TOKEN: 'fixture-review-token' };
	let planned = false;
	setup.runtime.plugins = async (_, child, run) => {
		planned = true;
		assert.equal(child.INPUT_TOKEN, undefined);
		return run();
	};
	assert.equal(await runAction(environment, setup.runtime), 0);
	assert.ok(planned);
	assert.equal(setup.executions[0]?.environment.INPUT_TOKEN, undefined);
	assert.equal(environment.INPUT_TOKEN, 'fixture-review-token');
});

test('actionlint PATH export can be disabled without disabling analysis', async () => {
	const setup = fixture();
	await runAction({ 'INPUT_ADD-ACTIONLINT-TO-PATH': 'false' }, setup.runtime);
	assert.deepEqual(setup.publications, []);
	assert.equal(setup.executions.length, 1);
});

test('Wasm check selection and invalid analysis booleans reach the native parser unchanged', async () => {
	for (const value of ['true', 'false', 'yes']) {
		const setup = fixture(value === 'yes' ? 2 : 0);
		assert.equal(
			await runAction({ INPUT_SHELLCHECK: value, INPUT_RUFF: value }, setup.runtime),
			value === 'yes' ? 2 : 0,
		);
		assert.deepEqual(setup.calls, ['native']);
		assert.equal(setup.executions[0]?.environment.INPUT_SHELLCHECK, value);
		assert.equal(setup.executions[0]?.environment.INPUT_RUFF, value);
	}
	assert.equal(toolEnabled('TRUE'), undefined);
	assert.equal(toolEnabled('false'), false);
	assert.equal(toolEnabled('true'), true);
});

test('explicit actionlint binary requires an absolute executable and bypasses downloading', async () => {
	const setup = fixture();
	const executable = join(tmpdir(), 'selected actionlint');
	await runAction({ ACTIONLINT_ACTION_BINARY: executable }, setup.runtime);
	assert.deepEqual(setup.calls, ['check-executable']);
	assert.equal(setup.executions[0]?.executable, executable);
	await assert.rejects(runAction({ ACTIONLINT_ACTION_BINARY: 'relative' }, setup.runtime), /absolute path/);
});

test('Windows input names are case-insensitive', { skip: process.platform !== 'win32' }, async () => {
	const setup = fixture();
	await runAction({ input_shellcheck: 'false', input_token: 'fixture-review-token' }, setup.runtime);
	assert.deepEqual(setup.executions[0]?.environment, {
		INPUT_SHELLCHECK: 'false',
		INPUT_PYFLAKES: 'false',
		INPUT_RUFF: 'true',
	});
});
