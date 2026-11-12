import assert from 'node:assert/strict';
import { test } from 'node:test';

import { writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { capture, temporary } from '#native';
import type { PluginCache } from '#plugin-cache';
import { withPluginCache } from '#plugin-cache';
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

test('selected legacy binary is rejected with or without plugin caching', async () => {
	await temporary(async (directory) => {
		const source = join(directory, 'legacy-actionlint.go');
		const executable = join(directory, process.platform === 'win32' ? 'legacy-actionlint.exe' : 'legacy-actionlint');
		await writeFile(
			source,
			[
				'package main',
				'import ("flag"; "fmt"; "os")',
				'func main() {',
				'  if len(os.Args) > 1 && os.Args[1] == "-github-action" {',
				'    fmt.Fprintln(os.Stdout, "analysis ran")',
				'    os.Exit(1)',
				'  }',
				'  flags := flag.NewFlagSet("actionlint", flag.ContinueOnError)',
				'  flags.SetOutput(os.Stderr)',
				'  flags.Bool("github-action", false, "run action mode")',
				'  if err := flags.Parse(os.Args[1:]); err != nil { os.Exit(2) }',
				'}',
			].join('\n'),
		);
		const build = await capture('go', ['build', '-o', executable, source], process.env, { timeoutMS: 300_000 });
		assert.equal(build.exitCode, 0, build.stderr);

		const unsupported = await capture(executable, ['-github-action-plugins']);
		assert.equal(unsupported.exitCode, 2);
		assert.match(unsupported.stderr, /^flag provided but not defined: -github-action-plugins\r?\nUsage of actionlint:/);
		assert.match(unsupported.stderr, /-github-action[\s\S]*run action mode/);

		const setup = fixture();
		const client: PluginCache = {
			restore: async () => assert.fail('unsupported binary must not restore plugin cache'),
			save: async () => assert.fail('unsupported binary must not save plugin cache'),
		};
		setup.runtime.native = async () => executable;
		setup.runtime.plugins = (selected, environment, run) =>
			withPluginCache(
				selected,
				environment,
				run,
				capture,
				client,
			);
		setup.runtime.execute = async (selected, args, environment) => {
			setup.executions.push({ executable: selected, args, environment });
			const result = await capture(selected, args, environment);
			assert.equal(result.stdout, 'analysis ran\n');
			return result.exitCode;
		};

		for (const environment of [{}, { 'INPUT_PLUGIN-CACHE': 'false' }]) {
			await assert.rejects(
				runAction(environment, setup.runtime),
				/requires an actionlint binary with Wasm plugin support/,
			);
		}
		assert.equal(setup.executions.length, 0);
		assert.equal(setup.publications.length, 0);
	});
});

test('install-only never inspects plugin configuration and cache input is validated', async () => {
	const installation = fixture();
	installation.runtime.plugins = async () => assert.fail('installation must not inspect plugins');
	assert.equal(await runAction({ 'INPUT_INSTALL-ONLY': 'true' }, installation.runtime), 0);
	const setup = fixture();
	await assert.rejects(runAction({ 'INPUT_PLUGIN-CACHE': 'sometimes' }, setup.runtime), InputError);
	assert.deepEqual(setup.calls, []);
});

test('ignored tool inputs pass through without warnings', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	const setup = fixture();
	const environment = { INPUT_PYFLAKES: 'true', INPUT_SHELLCHECK: 'false', INPUT_RUFF: 'false' };
	assert.equal(await runAction(environment, setup.runtime), 0);
	assert.deepEqual(setup.calls, ['native']);
	assert.deepEqual(setup.executions[0]?.environment, environment);
	assert.deepEqual(messages.filter((message) => message.startsWith('::warning::')), []);
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
		'INPUT_DISABLE-TOOLS': 'shellcheck',
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
		environment,
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

test('invalid analysis booleans reach the native parser unchanged', async () => {
	for (const value of ['true', 'false', 'yes']) {
		const setup = fixture(value === 'yes' ? 2 : 0);
		assert.equal(
			await runAction({ 'INPUT_FAIL-ON-ERROR': value }, setup.runtime),
			value === 'yes' ? 2 : 0,
		);
		assert.deepEqual(setup.calls, ['native']);
		assert.equal(setup.executions[0]?.environment['INPUT_FAIL-ON-ERROR'], value);
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
	await runAction({ 'input_disable-tools': 'ruff', input_token: 'fixture-review-token' }, setup.runtime);
	assert.deepEqual(setup.executions[0]?.environment, { 'INPUT_DISABLE-TOOLS': 'ruff' });
});
