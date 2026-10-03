import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createHash } from 'node:crypto';
import { chmod, copyFile, link, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { pyflakesVersion, runnerPlatform, shellcheckVersion } from '#assets';
import { capture, temporary, which } from '#native';
import { executeNative, pyflakesCommand, shellcheckBinary } from '#tools';

async function withEnvironment(values: Record<string, string>, run: () => Promise<void>): Promise<void> {
	const previous = new Map(Object.keys(values).map((name) => [name, process.env[name]]));
	try {
		for (const [name, value] of Object.entries(values)) process.env[name] = value;
		await run();
	} finally {
		for (const [name, value] of previous) {
			if (value === undefined) delete process.env[name];
			else process.env[name] = value;
		}
	}
}

test('native spawn preserves executable paths, arguments and exit status', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint exec-'));
	try {
		const name = process.platform === 'win32' ? 'node.exe' : 'node \\ " executable';
		const executable = join(directory, name);
		try {
			await link(process.execPath, executable);
		} catch {
			await copyFile(process.execPath, executable);
		}
		const args = ['a b', 'a"b', 'a\\b', '$(not-a-command)', '%PATH%', ''];
		const result = await capture(executable, [
			'-e',
			'process.stdout.write(JSON.stringify(process.argv.slice(1))); process.exitCode = 7;',
			'--',
			...args,
		]);
		assert.equal(result.exitCode, 7);
		assert.deepEqual(JSON.parse(result.stdout), args);
		assert.equal(result.stderr, '');
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('action execution uses only the supplied child environment and preserves exit codes', async () => {
	const previous = process.env.ACTIONLINT_TEST_SENTINEL;
	process.env.ACTIONLINT_TEST_SENTINEL = 'parent-only';
	try {
		await temporary(async (directory) => {
			const output = join(directory, 'environment.json');
			const code = await executeNative(process.execPath, [
				'-e',
				'require("node:fs").writeFileSync(process.argv[1], JSON.stringify(process.env)); process.exitCode = 3;',
				output,
			], { ACTIONLINT_TEST_LITERAL: 'a "b" \\ c', INPUT_TOKEN: 'fixture-review-token' });
			assert.equal(code, 3);
			const environment = JSON.parse(await readFile(output, 'utf8'));
			assert.equal(environment.ACTIONLINT_TEST_SENTINEL, undefined);
			assert.equal(environment.ACTIONLINT_TEST_LITERAL, 'a "b" \\ c');
			assert.equal(environment.INPUT_TOKEN, undefined);
			assert.equal(process.env.ACTIONLINT_TEST_SENTINEL, 'parent-only');
		});
	} finally {
		if (previous === undefined) delete process.env.ACTIONLINT_TEST_SENTINEL;
		else process.env.ACTIONLINT_TEST_SENTINEL = previous;
	}
});

test('tool probes exclude the review token from explicit and inherited environments', async () => {
	await withEnvironment({ INPUT_TOKEN: 'fixture-review-token' }, async () => {
		for (const environment of [undefined, { INPUT_TOKEN: 'fixture-review-token' }]) {
			const result = await capture(process.execPath, [
				'-e',
				'process.stdout.write(process.env.INPUT_TOKEN === undefined ? "absent" : "present")',
			], environment);
			assert.equal(result.exitCode, 0, result.stderr);
			assert.equal(result.stdout, 'absent');
		}
	});
});

test('native tools discovered on PATH report their version without changing selection', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	await temporary(async (directory) => {
		const extension = process.platform === 'win32' ? '.exe' : '';
		const shellcheck = join(directory, `shellcheck${extension}`);
		const pyflakes = join(directory, `pyflakes${extension}`);
		for (const executable of [shellcheck, pyflakes]) await copyFile(process.execPath, executable);
		await withEnvironment({ PATH: directory, PATHEXT: '.EXE' }, async () => {
			const platform = runnerPlatform(process.platform, process.arch);
			assert.deepEqual(await shellcheckBinary(platform), { kind: 'existing', executable: shellcheck });
			assert.deepEqual(await pyflakesCommand(platform, ''), { kind: 'existing', executable: pyflakes });
			assert.deepEqual(messages, [
				`::debug::ShellCheck: existing installation; version ${process.version}; ${shellcheck}`,
				`::debug::pyflakes: existing installation; version ${process.version}; ${pyflakes}`,
			]);
		});
	});
});

test('failed version probes retain the selected PATH executable', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	await temporary(async (directory) => {
		const extension = process.platform === 'win32' ? '.exe' : '';
		for (const name of ['shellcheck', 'pyflakes']) {
			const executable = join(directory, name + extension);
			await writeFile(executable, '#!/nonexistent/actionlint-test-interpreter\n');
			await chmod(executable, 0o755);
		}
		await withEnvironment({ PATH: directory, PATHEXT: '.EXE' }, async () => {
			const platform = runnerPlatform(process.platform, process.arch);
			assert.deepEqual(await shellcheckBinary(platform), {
				kind: 'existing',
				executable: join(directory, `shellcheck${extension}`),
			});
			assert.deepEqual(await pyflakesCommand(platform, ''), {
				kind: 'existing',
				executable: join(directory, `pyflakes${extension}`),
			});
		});
	});
	assert.equal(messages.length, 2);
	for (const message of messages) assert.match(message, /existing installation; version unavailable \(.+\);/);
});

test('PATH version probes read tool banners without exposing the review token', {
	skip: process.platform === 'win32',
}, async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	await temporary(async (directory) => {
		const shellcheck = join(directory, 'shellcheck');
		const pyflakes = join(directory, 'pyflakes');
		const guard = '#!/bin/sh\n[ "$1" = "--version" ] || exit 90\n[ -z "$INPUT_TOKEN" ] || exit 91\n';
		await writeFile(shellcheck, `${guard}printf 'ShellCheck - shell script analysis tool\\nversion: 0.11.0\\n'\n`);
		await writeFile(pyflakes, `${guard}printf '3.4.0 Python 3.13.0 on Linux\\n' >&2\n`);
		await chmod(shellcheck, 0o755);
		await chmod(pyflakes, 0o755);
		await withEnvironment({ PATH: directory, INPUT_TOKEN: 'fixture-review-token' }, async () => {
			const platform = runnerPlatform(process.platform, process.arch);
			await shellcheckBinary(platform);
			await pyflakesCommand(platform, '');
		});
		assert.deepEqual(messages, [
			`::debug::ShellCheck: existing installation; version 0.11.0; ${shellcheck}`,
			`::debug::pyflakes: existing installation; version 3.4.0; ${pyflakes}`,
		]);
	});
});

test('unsupported and timed-out version probes remain advisory', {
	skip: process.platform === 'win32',
}, async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	await temporary(async (directory) => {
		const executable = join(directory, 'shellcheck');
		for (
			const { script, reason } of [
				{ script: 'exit 7', reason: '--version exited 7' },
				{ script: 'exit 0', reason: 'empty --version output' },
				{ script: "printf 'custom wrapper\\n'", reason: 'unrecognized --version output' },
				{ script: 'while :; do :; done', reason: 'timed out after 1000ms' },
			]
		) {
			await writeFile(executable, `#!/bin/sh\n${script}\n`);
			await chmod(executable, 0o755);
			await withEnvironment({ PATH: directory }, async () => {
				assert.deepEqual(await shellcheckBinary(runnerPlatform(process.platform, process.arch)), {
					kind: 'existing',
					executable,
				});
			});
			assert.ok(messages.at(-1)?.includes(reason));
		}
	});
});

test('Windows Python batch shims resolve to native Python before forwarding arbitrary arguments', {
	skip: process.platform !== 'win32',
}, async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	const installed = await which('python3', process.env, 'native') || await which('python', process.env, 'native');
	assert.ok(installed, 'Python is required to verify shim discovery');
	const probe = await capture(installed, ['-I', '-c', 'import sys; print(sys.executable)']);
	assert.equal(probe.exitCode, 0, probe.stderr);
	const python = probe.stdout.trim();
	for (const launcher of ['python3.cmd', 'python.bat', 'py.cmd']) {
		await temporary(async (directory) => {
			const shims = join(directory, 'Python shims & %PATH% !bang! ^caret (test)');
			await mkdir(shims);
			const selectPython = launcher === 'py.cmd' ? 'if not "%~1"=="-3" exit /b 92\r\n' : '';
			const shimArguments = launcher === 'py.cmd' ? '%2 %3 %4' : '%*';
			await writeFile(
				join(shims, launcher),
				`@echo off\r\n${selectPython}"%ACTIONLINT_TEST_PYTHON%" ${shimArguments}\r\nexit /b %errorlevel%\r\n`,
			);
			for (const name of ['shellcheck.cmd', 'pyflakes.bat']) {
				await writeFile(join(shims, name), '@exit /b 91\r\n');
			}
			const cache = join(directory, 'cache');
			const legacyPyflakes = join(cache, 'actionlint-pyflakes', pyflakesVersion, 'any');
			const shellcheck = join(cache, 'actionlint-shellcheck-windows', shellcheckVersion, 'amd64');
			await mkdir(legacyPyflakes, { recursive: true });
			await mkdir(shellcheck, { recursive: true });
			await writeFile(`${legacyPyflakes}.complete`, '');
			await writeFile(join(legacyPyflakes, 'actionlint-pyflakes.py'), 'raise RuntimeError("stale launcher")\n');
			await writeFile(`${shellcheck}.complete`, '');
			await writeFile(join(shellcheck, 'shellcheck.exe'), 'cached native executable');
			const launcherSource =
				'import json, sys\nprint(json.dumps({"isolated": sys.flags.isolated, "args": sys.argv[1:]}))\n';
			const launchers = new Map<string, string>();
			for (const source of [launcherSource, `# Revised launcher\n${launcherSource}`]) {
				const digest = createHash('sha256').update(source).digest('hex');
				const pyflakes = join(cache, `actionlint-pyflakes-${digest}`, pyflakesVersion, 'any');
				await mkdir(pyflakes, { recursive: true });
				await writeFile(`${pyflakes}.complete`, '');
				const script = join(pyflakes, 'actionlint-pyflakes.py');
				await writeFile(script, source);
				launchers.set(source, script);
			}
			await withEnvironment({
				PATH: shims,
				PATHEXT: '.CMD;.BAT;.COM;.EXE',
				RUNNER_TOOL_CACHE: cache,
				ACTIONLINT_TEST_PYTHON: python,
			}, async () => {
				assert.deepEqual(await shellcheckBinary({ os: 'windows', arch: 'amd64' }), {
					kind: 'standalone',
					executable: join(shellcheck, 'shellcheck.exe'),
				});
				for (const [source, script] of launchers) {
					const command = await pyflakesCommand({ os: 'windows', arch: 'amd64' }, source);
					if (command.kind !== 'python') assert.fail('batch Pyflakes must use the isolated wheel fallback');
					assert.equal(command.executable, python);
					assert.equal(command.script, script);
					const args = ['a b', 'a"b', 'trailing\\', 'a&b', '%PATH%', '!VALUE!', '(x)|<y>', ''];
					const result = await capture(command.executable, ['-I', command.script, ...args]);
					assert.equal(result.exitCode, 0, result.stderr);
					assert.deepEqual(JSON.parse(result.stdout), { isolated: 1, args });
				}
			});
		});
	}
	assert.ok(
		messages.some((message) => message.startsWith(`::debug::ShellCheck: tool cache; version ${shellcheckVersion};`)),
	);
	assert.ok(
		messages.some((message) => message.startsWith(`::debug::pyflakes: tool cache; version ${pyflakesVersion};`)),
	);
});
