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
		inspect: async () => ({ shellcheck: true, ruff: false }),
		shellcheck: async () => {
			calls.push('shellcheck');
			return { kind: 'standalone', executable: join(tmpdir(), 'tools with spaces', 'shellcheck') };
		},
		ruff: async () => {
			calls.push('ruff');
			return { kind: 'standalone', executable: join(tmpdir(), 'tools with spaces', 'ruff') };
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

test('deprecated Python input warns and cannot enable Pyflakes in a selected older binary', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	const setup = fixture();
	assert.equal(await runAction({ INPUT_PYFLAKES: 'true', INPUT_SHELLCHECK: 'false' }, setup.runtime), 0);
	assert.deepEqual(setup.calls, ['native']);
	assert.equal(setup.executions[0]?.environment.INPUT_PYFLAKES, 'false');
	assert.deepEqual(Object.keys(setup.publications[0] ?? {}), ['actionlint']);
	assert.ok(
		messages.some((message) => message.startsWith('::warning::') && message.includes('pyflakes input is ignored')),
	);
});

test('retired Pyflakes PATH requests warn once without enabling installation', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	for (const setupOnly of [false, true]) {
		for (const value of [undefined, '', 'false', 'true']) {
			for (const pyflakes of ['false', 'true']) {
				messages.length = 0;
				const setup = fixture();
				const environment: Environment = {
					'INPUT_INSTALL-ONLY': String(setupOnly),
					INPUT_SHELLCHECK: 'false',
					INPUT_RUFF: 'false',
					INPUT_PYFLAKES: pyflakes,
				};
				if (value !== undefined) environment['INPUT_ADD-PYFLAKES-TO-PATH'] = value;
				assert.equal(await runAction(environment, setup.runtime), 0);
				const warnings = messages.filter((message) => message.startsWith('::warning::'));
				assert.equal(warnings.length, Number(value === 'true') + Number(pyflakes === 'true'));
				assert.equal(warnings.some((message) => message.includes('add-pyflakes-to-path')), value === 'true');
				assert.deepEqual(setup.calls, ['native']);
				assert.equal(environment['INPUT_ADD-PYFLAKES-TO-PATH'], value);
				if (!setupOnly) {
					assert.equal(setup.executions[0]?.environment.INPUT_PYFLAKES, 'false');
					assert.equal(
						setup.executions[0]?.environment['INPUT_ADD-PYFLAKES-TO-PATH'],
						value === 'true' ? 'false' : value,
					);
				}
			}
		}
	}
});

test('install-only publishes tools without inspecting or executing the selected binary', async () => {
	const setup = fixture();
	setup.runtime.inspect = async () => assert.fail('setup must not inspect workflows or require the Action protocol');
	setup.runtime.execute = async () => assert.fail('setup must not analyze');
	assert.equal(await runAction({ 'INPUT_INSTALL-ONLY': 'true', INPUT_CONFIG: 'not linted' }, setup.runtime), 0);
	assert.deepEqual(setup.calls, ['native', 'shellcheck', 'ruff']);
	assert.deepEqual(Object.keys(setup.publications[0] ?? {}).sort(), ['actionlint', 'ruff', 'shellcheck']);
});

test('install-only respects optional-tool selection and validates setup inputs before downloading', async () => {
	const setup = fixture();
	assert.equal(
		await runAction(
			{ 'INPUT_INSTALL-ONLY': 'true', INPUT_SHELLCHECK: 'false', INPUT_RUFF: 'false', INPUT_PYFLAKES: 'false' },
			setup.runtime,
		),
		0,
	);
	assert.deepEqual(setup.calls, ['native']);
	assert.deepEqual(setup.executions, []);
	for (
		const environment of [
			{ 'INPUT_INSTALL-ONLY': 'yes' },
			{ 'INPUT_INSTALL-ONLY': 'true', INPUT_SHELLCHECK: 'yes' },
			{ 'INPUT_INSTALL-ONLY': 'true', INPUT_RUFF: 'yes' },
			{ 'INPUT_INSTALL-ONLY': 'true', 'INPUT_ADD-ACTIONLINT-TO-PATH': 'false' },
		]
	) {
		const invalid = fixture();
		await assert.rejects(runAction(environment, invalid.runtime), InputError);
		assert.deepEqual(invalid.calls, []);
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
	assert.deepEqual(setup.publications, [{ actionlint: join(tmpdir(), 'downloaded actionlint') }]);
	assert.deepEqual(setup.executions, [{
		executable: join(tmpdir(), 'downloaded actionlint'),
		args: ['-github-action'],
		environment: { ...environment, INPUT_RUFF: 'true' },
	}]);
});

test('default enabled tools provision commands only in the native child environment', async () => {
	const setup = fixture();
	const environment = { INPUT_CONFIG: '{}' };
	await runAction(environment, setup.runtime);
	const child = setup.executions[0]?.environment;
	assert.equal(child?.ACTIONLINT_SHELLCHECK_COMMAND, join(tmpdir(), 'tools with spaces', 'shellcheck'));
	assert.deepEqual(environment, { INPUT_CONFIG: '{}' });
	assert.deepEqual(setup.publications, [{
		actionlint: join(tmpdir(), 'downloaded actionlint'),
		shellcheck: { kind: 'standalone', executable: join(tmpdir(), 'tools with spaces', 'shellcheck') },
	}]);
});

test('preflight and analysis exclude the review token while the reporter retains it', async () => {
	for (const review of ['false', 'true']) {
		const setup = fixture();
		const environment = { INPUT_TOKEN: 'fixture-review-token', INPUT_REVIEW: review };
		let inspected = false;
		setup.runtime.inspect = async (_, child) => {
			inspected = true;
			assert.equal(child.INPUT_TOKEN, undefined);
			return { shellcheck: false, ruff: false };
		};
		assert.equal(await runAction(environment, setup.runtime), 0);
		assert.ok(inspected);
		assert.equal(setup.executions[0]?.environment.INPUT_TOKEN, undefined);
		assert.equal(environment.INPUT_TOKEN, 'fixture-review-token');
	}
});

test('each PATH export can be disabled independently without disabling lint tools', async () => {
	for (const disabled of ['actionlint', 'shellcheck']) {
		const setup = fixture();
		await runAction({ [`INPUT_ADD-${disabled.toUpperCase()}-TO-PATH`]: 'false' }, setup.runtime);
		const expected = ['actionlint', 'shellcheck'].filter((tool) => tool !== disabled);
		assert.deepEqual(Object.keys(setup.publications[0] ?? {}).sort(), expected.sort());
		const child = setup.executions[0]?.environment;
		assert.equal(child?.ACTIONLINT_SHELLCHECK_COMMAND, join(tmpdir(), 'tools with spaces', 'shellcheck'));
	}
	const setup = fixture();
	await runAction({
		'INPUT_ADD-ACTIONLINT-TO-PATH': 'false',
		'INPUT_ADD-SHELLCHECK-TO-PATH': 'false',
	}, setup.runtime);
	assert.deepEqual(setup.publications, []);
	assert.equal(setup.executions.length, 1);
	assert.equal(
		setup.executions[0]?.environment.ACTIONLINT_SHELLCHECK_COMMAND,
		join(tmpdir(), 'tools with spaces', 'shellcheck'),
	);
	await assert.rejects(runAction({ 'INPUT_ADD-SHELLCHECK-TO-PATH': 'yes' }, setup.runtime), InputError);
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
	setup.runtime.shellcheck = async () => {
		throw new Error('missing ShellCheck');
	};
	await assert.rejects(runAction({}, setup.runtime), /missing ShellCheck/);
	assert.deepEqual(setup.executions, []);
});

test('effective configuration disables provisioning and PATH export before tool lookup', async () => {
	const setup = fixture();
	setup.runtime.inspect = async (_, environment) => {
		assert.equal(environment.ACTIONLINT_SHELLCHECK_COMMAND, undefined);
		assert.equal(environment.INPUT_CONFIG, 'tools: {shellcheck: {enabled: false}}');
		return { shellcheck: false, ruff: false };
	};
	setup.runtime.shellcheck = async () => {
		assert.fail('disabled ShellCheck must never be located or downloaded');
	};
	await runAction({
		INPUT_CONFIG: 'tools: {shellcheck: {enabled: false}}',
		ACTIONLINT_SHELLCHECK_COMMAND: 'stale override',
	}, setup.runtime);
	assert.deepEqual(setup.calls, ['native']);
	assert.equal(setup.publications[0]?.shellcheck, undefined);
	assert.equal(setup.executions[0]?.environment.ACTIONLINT_SHELLCHECK_COMMAND, undefined);
	assert.equal(setup.executions[0]?.environment.INPUT_CONFIG, 'tools: {shellcheck: {enabled: false}}');
});

test('invalid effective configuration stops before optional tool installation', async () => {
	const setup = fixture();
	setup.runtime.inspect = async () => {
		throw new InputError('invalid tools input');
	};
	await assert.rejects(runAction({ INPUT_CONFIG: 'invalid' }, setup.runtime), InputError);
	assert.deepEqual(setup.calls, ['native']);
	assert.deepEqual(setup.publications, []);
	assert.deepEqual(setup.executions, []);
});

test('Windows input and tool override names are case-insensitive', { skip: process.platform !== 'win32' }, async () => {
	const setup = fixture();
	await runAction({
		input_shellcheck: 'false',
		input_pyflakes: 'false',
		input_token: 'fixture-review-token',
		actionlint_shellcheck_command: 'stale ShellCheck',
	}, setup.runtime);
	assert.deepEqual(setup.calls, ['native']);
	assert.deepEqual(setup.executions[0]?.environment, {
		INPUT_SHELLCHECK: 'false',
		INPUT_PYFLAKES: 'false',
		INPUT_RUFF: 'true',
	});
});

test('Ruff provisions independently of ShellCheck and honors its independent PATH switch', async () => {
	for (const publish of [true, false]) {
		const setup = fixture();
		setup.runtime.inspect = async () => ({ shellcheck: false, ruff: true });
		await runAction({ INPUT_SHELLCHECK: 'false', 'INPUT_ADD-RUFF-TO-PATH': String(publish) }, setup.runtime);
		assert.deepEqual(setup.calls, ['native', 'ruff']);
		assert.equal(setup.executions[0]?.environment.ACTIONLINT_RUFF_COMMAND, join(tmpdir(), 'tools with spaces', 'ruff'));
		assert.equal(Boolean(setup.publications[0]?.ruff), publish);
	}
});

test('Ruff disabled by input or effective per-file configuration is never provisioned', async () => {
	for (const input of ['true', 'false']) {
		const setup = fixture();
		setup.runtime.inspect = async (_, environment) => {
			assert.equal(environment.ACTIONLINT_RUFF_COMMAND, undefined);
			return { shellcheck: false, ruff: input === 'false' };
		};
		setup.runtime.ruff = async () => assert.fail('disabled Ruff must not be located, downloaded or cached');
		await runAction({ INPUT_RUFF: input, ACTIONLINT_RUFF_COMMAND: 'stale Ruff' }, setup.runtime);
		assert.deepEqual(setup.calls, ['native']);
		assert.equal(setup.publications[0]?.ruff, undefined);
		assert.equal(setup.executions[0]?.environment.ACTIONLINT_RUFF_COMMAND, undefined);
	}
});

test('Ruff provisioning errors stop analysis and invalid input skips all optional installation', async () => {
	const setup = fixture();
	setup.runtime.inspect = async () => ({ shellcheck: false, ruff: true });
	setup.runtime.ruff = async () => {
		throw new Error('Ruff SHA-256 checksum mismatch');
	};
	await assert.rejects(runAction({}, setup.runtime), /Ruff SHA-256 checksum mismatch/);
	assert.deepEqual(setup.executions, []);
	const invalid = fixture(2);
	invalid.runtime.inspect = async () => assert.fail('invalid input must not inspect or provision');
	assert.equal(await runAction({ INPUT_RUFF: 'yes' }, invalid.runtime), 2);
	assert.deepEqual(invalid.calls, ['native']);
	assert.equal(invalid.executions[0]?.environment.INPUT_RUFF, 'yes');
});
