import assert from 'node:assert/strict';
import { test } from 'node:test';

import { spawnSync } from 'node:child_process';
import { copyFile, mkdir, readdir, readFile, rm, symlink, unlink, writeFile } from 'node:fs/promises';
import { basename, delimiter, dirname, join } from 'node:path';

import { capture, temporary, which } from '#native';
import { publishTools } from '#path';

function withPath(directories: string[]): NodeJS.ProcessEnv {
	const environment = Object.fromEntries(
		Object.entries(process.env).filter(([key]) => key.toLowerCase() !== 'path'),
	);
	environment.PATH = [...directories, process.env.PATH ?? ''].join(delimiter);
	return environment;
}

test('published actionlint stays runnable after step cleanup and each publication uses a fresh directory', async () => {
	await temporary(async (job) => {
		const pathFile = join(job, 'github-path');
		const step = join(job, 'step');
		await mkdir(step);
		const binary = join(step, 'actionlint');
		await copyFile(process.execPath, binary);
		const existing = join(job, 'existing tools');
		await mkdir(existing);
		await writeFile(join(existing, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint'), 'wrong binary', {
			mode: 0o755,
		});
		const tools = { actionlint: binary };
		await publishTools(tools, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		await publishTools(tools, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		await rm(step, { recursive: true });
		const paths = (await readFile(pathFile, 'utf8')).trim().split('\n');
		assert.equal(paths.length, 2);
		assert.notEqual(paths[0], paths[1]);
		for (const publication of [paths.slice(0, 1), paths]) {
			const result = spawnSync(
				'actionlint',
				['-e', 'process.stdout.write("next step");'],
				{ cwd: job, env: withPath([...publication].reverse().concat(existing)), encoding: 'utf8', windowsHide: true },
			);
			assert.equal(result.status, 0, result.stderr || result.error?.message);
			assert.equal(result.stdout, 'next step');
		}
	});
});

test('published pyflakes wrapper preserves spaced Python paths, arguments and isolated mode', async () => {
	await temporary(async (job) => {
		const command = await which('python3', process.env, 'native') || await which('python', process.env, 'native');
		assert.ok(command, 'Python is required to verify the pyflakes wrapper');
		const probe = await capture(command, ['-I', '-c', 'import sys; print(sys.executable)']);
		assert.equal(probe.exitCode, 0, probe.stderr);
		const python = probe.stdout.trim();
		const linkedDirectory = join(job, 'python with spaces');
		await symlink(dirname(python), linkedDirectory, process.platform === 'win32' ? 'junction' : 'dir');
		try {
			const script = join(job, 'pyflakes script.py');
			await writeFile(
				script,
				'import json, sys\nprint(json.dumps({"isolated": sys.flags.isolated, "args": sys.argv[1:]}))\nsys.exit(7)\n',
			);
			const pathFile = join(job, 'github-path');
			await publishTools({
				pyflakes: { kind: 'python', executable: join(linkedDirectory, basename(python)), script },
			}, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
			const directory = (await readFile(pathFile, 'utf8')).trim();
			assert.equal((await readdir(directory)).some((name) => name.startsWith('actionlint')), false);
			const environment = withPath([directory]);
			const nextStep = join(job, 'next-step.cmd');
			if (process.platform === 'win32') {
				await writeFile(nextStep, '@call pyflakes "argument with spaces" second\r\n@exit /b %errorlevel%\r\n');
			}
			const result = process.platform === 'win32'
				? await capture('cmd.exe', ['/d', '/c', nextStep], environment)
				: await capture('pyflakes', ['argument with spaces', 'second'], environment);
			assert.equal(result.exitCode, 7, result.stderr);
			assert.deepEqual(JSON.parse(result.stdout), { isolated: 1, args: ['argument with spaces', 'second'] });
			if (process.platform === 'win32') {
				const powershell = await which('pwsh', process.env, 'native')
					|| await which('powershell', process.env, 'native');
				assert.ok(powershell, 'PowerShell is required to verify next-step tool exports');
				const powershellResult = await capture(powershell, [
					'-NoLogo',
					'-NoProfile',
					'-NonInteractive',
					'-Command',
					'& pyflakes "argument with spaces" second; exit $LASTEXITCODE',
				], environment);
				assert.equal(powershellResult.exitCode, 7, powershellResult.stderr);
				assert.deepEqual(JSON.parse(powershellResult.stdout), {
					isolated: 1,
					args: ['argument with spaces', 'second'],
				});
				const git = await which('git', process.env, 'native');
				assert.ok(git, 'Git for Windows is required to verify the Bash wrapper');
				const shell = await capture(git, ['var', 'GIT_SHELL_PATH']);
				assert.equal(shell.exitCode, 0, shell.stderr);
				const bash = join(dirname(shell.stdout.trim()), 'bash.exe');
				const bashResult = await capture(bash, ['-c', 'pyflakes "argument with spaces" second'], environment);
				assert.equal(bashResult.exitCode, 7, bashResult.stderr);
				assert.deepEqual(JSON.parse(bashResult.stdout), { isolated: 1, args: ['argument with spaces', 'second'] });
			}
		} finally {
			await unlink(linkedDirectory);
		}
	});
});

test('exported ShellCheck preserves spaced paths and arguments in subsequent steps', async () => {
	await temporary(async (job) => {
		const directory = join(job, 'tools with spaces');
		await mkdir(directory);
		const shellcheck = join(directory, process.platform === 'win32' ? 'shellcheck.exe' : 'shellcheck');
		await copyFile(process.execPath, shellcheck);
		const pathFile = join(job, 'github-path');
		await publishTools({ shellcheck: { kind: 'standalone', executable: shellcheck } }, {
			RUNNER_TEMP: job,
			GITHUB_PATH: pathFile,
		});
		const environment = withPath((await readFile(pathFile, 'utf8')).trim().split('\n'));
		const script = join(job, 'argument probe.cjs');
		await writeFile(script, 'process.stdout.write(JSON.stringify(process.argv.slice(2))); process.exitCode = 7;');
		const args = ['script with spaces.sh', 'second.sh'];
		const result = await capture('shellcheck', [script, ...args], environment);
		assert.equal(result.exitCode, 7, result.stderr);
		assert.deepEqual(JSON.parse(result.stdout), args);
		if (process.platform === 'win32') {
			const powershell = await which('pwsh', process.env, 'native') || await which('powershell', process.env, 'native');
			assert.ok(powershell, 'PowerShell is required to verify next-step tool exports');
			const result = await capture(powershell, [
				'-NoLogo',
				'-NoProfile',
				'-NonInteractive',
				'-Command',
				'& shellcheck $env:ACTIONLINT_TEST_SCRIPT "script with spaces.sh" second.sh; exit $LASTEXITCODE',
			], { ...environment, ACTIONLINT_TEST_SCRIPT: script });
			assert.equal(result.exitCode, 7, result.stderr);
			assert.deepEqual(JSON.parse(result.stdout), args);
		}
	});
});

test('publication isolates selected tools from conflicting siblings without changing unrelated commands', async () => {
	await temporary(async (job) => {
		const configured = join(job, 'configured tools');
		const shellcheckDirectory = join(job, 'shellcheck toolchain');
		const pyflakesDirectory = join(job, 'pyflakes toolchain');
		for (const directory of [configured, shellcheckDirectory, pyflakesDirectory]) await mkdir(directory);
		const extension = process.platform === 'win32' ? '.exe' : '';
		const shellcheck = join(shellcheckDirectory, `shellcheck${extension}`);
		const pyflakes = join(pyflakesDirectory, `pyflakes${extension}`);
		const unrelated = join(configured, `unrelated${extension}`);
		for (const executable of [shellcheck, pyflakes, unrelated]) await copyFile(process.execPath, executable);
		for (
			const { directory, name } of [
				{ directory: shellcheckDirectory, name: 'pyflakes' },
				{ directory: pyflakesDirectory, name: 'shellcheck' },
				{ directory: pyflakesDirectory, name: 'actionlint' },
				{ directory: pyflakesDirectory, name: 'unrelated' },
			]
		) {
			await writeFile(join(directory, `${name}${extension}`), 'wrong binary', { mode: 0o755 });
		}
		const pathFile = join(job, 'github-path');
		await publishTools({
			actionlint: process.execPath,
			shellcheck: { kind: 'standalone', executable: shellcheck },
			pyflakes: { kind: 'existing', executable: pyflakes },
		}, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		const paths = (await readFile(pathFile, 'utf8')).trim().split('\n');
		assert.equal(paths.length, 1);
		assert.notEqual(paths[0], shellcheckDirectory);
		assert.notEqual(paths[0], pyflakesDirectory);
		const environment = withPath([...paths, configured, pyflakesDirectory, shellcheckDirectory]);
		assert.equal(await which('unrelated', environment), unrelated);
		assert.equal(await which('pyflakes', environment), pyflakes);
		for (const name of ['actionlint', 'shellcheck', 'pyflakes', 'unrelated']) {
			const result = await capture(
				name,
				['-e', 'process.stdout.write("selected"); process.exitCode = 7;'],
				environment,
			);
			assert.equal(result.exitCode, 7, `${name}: ${result.stderr}`);
			assert.equal(result.stdout, 'selected', name);
		}
	});
});

test('existing command scripts retain their PATH location and sibling dependencies', async () => {
	await temporary(async (job) => {
		const directory = join(job, "selected 'tools' with spaces");
		await mkdir(directory);
		const script = join(directory, 'probe.cjs');
		await writeFile(script, 'process.stdout.write(JSON.stringify(process.argv.slice(2))); process.exitCode = 7;');
		const extension = process.platform === 'win32' ? '.cmd' : '';
		const pyflakes = join(directory, `pyflakes${extension}`);
		const command = process.platform === 'win32'
			? `@echo off\r\n"${process.execPath}" "%~dp0probe.cjs" %*\r\nexit /b %errorlevel%\r\n`
			: `#!/bin/sh\nexec '${process.execPath.replaceAll("'", "'\\''")}' "\${0%/*}/probe.cjs" "$@"\n`;
		await writeFile(pyflakes, command, { mode: 0o755 });
		const pathFile = join(job, 'github-path');
		await publishTools({ pyflakes: { kind: 'existing', executable: pyflakes } }, {
			RUNNER_TEMP: job,
			GITHUB_PATH: pathFile,
		});
		await assert.rejects(readFile(pathFile), { code: 'ENOENT' });
		const environment = withPath([directory]);
		assert.equal(await which('pyflakes', environment), pyflakes);
		const args = ['argument with spaces', 'second'];
		const nextStep = join(job, 'next-step.cmd');
		if (process.platform === 'win32') {
			await writeFile(nextStep, '@call pyflakes "argument with spaces" second\r\n@exit /b %errorlevel%\r\n');
		}
		const result = process.platform === 'win32'
			? await capture('cmd.exe', ['/d', '/c', nextStep], environment)
			: await capture('pyflakes', args, environment);
		assert.equal(result.exitCode, 7, result.stderr);
		assert.deepEqual(JSON.parse(result.stdout), args);
	});
});

test('Windows existing commands retain native extensions and PATHEXT lookup', {
	skip: process.platform !== 'win32',
}, async () => {
	await temporary(async (job) => {
		const command = join(job, 'shellcheck.COM');
		await copyFile(process.execPath, command);
		const pathFile = join(job, 'github-path');
		await publishTools({ shellcheck: { kind: 'existing', executable: command } }, {
			RUNNER_TEMP: job,
			GITHUB_PATH: pathFile,
		});
		await assert.rejects(readFile(pathFile), { code: 'ENOENT' });
		const environment = { ...withPath([job]), PATHEXT: '.COM;.EXE' };
		const executable = await which('shellcheck', environment, 'native');
		assert.equal(executable.toLowerCase(), command.toLowerCase());
		const result = await capture(executable, ['-e', 'process.stdout.write("native");'], environment);
		assert.equal(result.exitCode, 0, result.stderr);
		assert.equal(result.stdout, 'native');
	});
});

test('Windows existing native tools retain executable-local resources while actionlint is exported', {
	skip: process.platform !== 'win32',
}, async () => {
	await temporary(async (job) => {
		const existing = join(job, 'existing tools');
		await mkdir(existing);
		const shellcheck = join(existing, 'shellcheck.exe');
		const pyflakes = join(existing, 'pyflakes.exe');
		for (const executable of [shellcheck, pyflakes]) await copyFile(process.execPath, executable);
		await writeFile(join(existing, 'runtime-resource'), 'native resources');
		const args = [
			'-e',
			'process.stdout.write(require("node:fs").readFileSync(require("node:path").join(require("node:path").dirname(process.execPath), "runtime-resource"))); process.exitCode = 7;',
		];
		const originalEnvironment = withPath([existing]);
		for (const name of ['shellcheck', 'pyflakes']) {
			const original = await capture(name, args, originalEnvironment);
			assert.equal(original.exitCode, 7, original.stderr);
			assert.equal(original.stdout, 'native resources');
		}
		const pathFile = join(job, 'github-path');
		await publishTools({
			actionlint: process.execPath,
			shellcheck: { kind: 'existing', executable: shellcheck },
			pyflakes: { kind: 'existing', executable: pyflakes },
		}, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		const directory = (await readFile(pathFile, 'utf8')).trim();
		assert.deepEqual(await readdir(directory), ['actionlint.exe']);
		const environment = withPath([directory, existing]);
		for (const name of ['shellcheck', 'pyflakes']) {
			const executable = await which(name, environment, 'native');
			assert.equal(executable, join(existing, `${name}.exe`));
			const result = await capture(executable, args, environment);
			assert.equal(result.exitCode, 7, result.stderr);
			assert.equal(result.stdout, 'native resources');
		}
	});
});

test('publishing no tools leaves PATH untouched and failed publication cleans its artifacts', async () => {
	await temporary(async (job) => {
		const pathFile = join(job, 'github-path');
		await publishTools({}, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		assert.deepEqual(await readdir(job), []);
		await assert.rejects(
			publishTools({ shellcheck: { kind: 'standalone', executable: process.execPath } }, { GITHUB_PATH: pathFile }),
			/RUNNER_TEMP/,
		);
		await assert.rejects(
			publishTools({ shellcheck: { kind: 'standalone', executable: process.execPath } }, {
				RUNNER_TEMP: job,
				GITHUB_PATH: join(job, 'missing directory', 'github-path'),
			}),
			/ENOENT/,
		);
		assert.deepEqual(await readdir(job), []);
	});
});
