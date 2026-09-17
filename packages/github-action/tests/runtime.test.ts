import assert from 'node:assert/strict';
import { test } from 'node:test';

import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { Environment, InstalledTools, Runtime } from '#runtime';
import { InputError, runAction, toolEnabled } from '#runtime';

function fixture(exitCode = 0) {
	const calls: string[] = [];
	const publications: InstalledTools[] = [];
	const executions: { executable: string; args: string[]; environment: Environment }[] = [];
	const runtime: Runtime = {
		native: async () => {
			calls.push('native');
			return join(tmpdir(), 'downloaded actionlint');
		},
		checkExecutable: async () => {
			calls.push('check-executable');
		},
		shellcheck: async () => {
			calls.push('shellcheck');
			return join(tmpdir(), 'tools with spaces', 'shellcheck');
		},
		pyflakes: async () => {
			calls.push('pyflakes');
			return { kind: 'python', executable: join(tmpdir(), 'python'), script: join(tmpdir(), 'pyflakes.py') };
		},
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
	assert.deepEqual(setup.publications, [{ actionlint: join(tmpdir(), 'downloaded actionlint') }]);
	assert.deepEqual(setup.executions, [{
		executable: join(tmpdir(), 'downloaded actionlint'),
		args: ['-github-action'],
		environment,
	}]);
});

test('default enabled tools provision commands only in the native child environment', async () => {
	const setup = fixture();
	const environment = { INPUT_CONFIG: '{}' };
	await runAction(environment, setup.runtime);
	const child = setup.executions[0]?.environment;
	assert.equal(child?.ACTIONLINT_PYTHON, join(tmpdir(), 'python'));
	assert.equal(child?.ACTIONLINT_PYFLAKES_SCRIPT, join(tmpdir(), 'pyflakes.py'));
	assert.equal(child?.ACTIONLINT_SHELLCHECK_COMMAND, join(tmpdir(), 'tools with spaces', 'shellcheck'));
	assert.deepEqual(environment, { INPUT_CONFIG: '{}' });
	assert.deepEqual(setup.publications, [{
		actionlint: join(tmpdir(), 'downloaded actionlint'),
		shellcheck: join(tmpdir(), 'tools with spaces', 'shellcheck'),
		pyflakes: { kind: 'python', executable: join(tmpdir(), 'python'), script: join(tmpdir(), 'pyflakes.py') },
	}]);
});

test('each PATH export can be disabled independently without disabling lint tools', async () => {
	for (const disabled of ['actionlint', 'shellcheck', 'pyflakes']) {
		const setup = fixture();
		await runAction({ [`INPUT_ADD-${disabled.toUpperCase()}-TO-PATH`]: 'false' }, setup.runtime);
		const expected = ['actionlint', 'shellcheck', 'pyflakes'].filter((tool) => tool !== disabled);
		assert.deepEqual(Object.keys(setup.publications[0] ?? {}).sort(), expected.sort());
		const child = setup.executions[0]?.environment;
		assert.equal(child?.ACTIONLINT_SHELLCHECK_COMMAND, join(tmpdir(), 'tools with spaces', 'shellcheck'));
		assert.equal(child?.ACTIONLINT_PYTHON, join(tmpdir(), 'python'));
	}
	const setup = fixture();
	await runAction({
		'INPUT_ADD-ACTIONLINT-TO-PATH': 'false',
		'INPUT_ADD-SHELLCHECK-TO-PATH': 'false',
		'INPUT_ADD-PYFLAKES-TO-PATH': 'false',
	}, setup.runtime);
	assert.deepEqual(setup.publications, []);
	assert.equal(setup.executions.length, 1);
	assert.equal(setup.executions[0]?.environment.ACTIONLINT_PYTHON, join(tmpdir(), 'python'));
	assert.equal(
		setup.executions[0]?.environment.ACTIONLINT_SHELLCHECK_COMMAND,
		join(tmpdir(), 'tools with spaces', 'shellcheck'),
	);
	await assert.rejects(runAction({ 'INPUT_ADD-PYFLAKES-TO-PATH': 'yes' }, setup.runtime), InputError);
});

test('existing pyflakes executable is passed directly', async () => {
	const setup = fixture();
	setup.runtime.pyflakes = async () => ({ kind: 'command', executable: join(tmpdir(), 'pyflakes.exe') });
	await runAction({
		INPUT_SHELLCHECK: 'false',
		ACTIONLINT_PYTHON: 'stale Python override',
		ACTIONLINT_PYFLAKES_SCRIPT: 'stale script override',
	}, setup.runtime);
	assert.equal(setup.executions[0]?.environment.ACTIONLINT_PYFLAKES_COMMAND, join(tmpdir(), 'pyflakes.exe'));
	assert.equal(setup.executions[0]?.environment.ACTIONLINT_PYTHON, undefined);
	assert.equal(setup.executions[0]?.environment.ACTIONLINT_PYFLAKES_SCRIPT, undefined);
});

test('invalid booleans are forwarded without provisioning optional tools', async () => {
	const setup = fixture(2);
	assert.equal(await runAction({ INPUT_SHELLCHECK: 'yes' }, setup.runtime), 2);
	assert.equal(setup.calls.includes('shellcheck'), false);
	assert.equal(setup.calls.includes('pyflakes'), false);
	assert.equal(setup.executions[0]?.environment.INPUT_SHELLCHECK, 'yes');
	assert.equal(toolEnabled('TRUE'), undefined);
	assert.equal(toolEnabled('false'), false);
	assert.equal(toolEnabled('true'), true);
});

test('enabled tool installation failures stop execution', async () => {
	const setup = fixture();
	setup.runtime.pyflakes = async () => {
		throw new Error('missing Python');
	};
	await assert.rejects(runAction({ INPUT_SHELLCHECK: 'false' }, setup.runtime), /missing Python/);
	assert.deepEqual(setup.executions, []);
});
