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
		const tools = { actionlint: binary, shellcheck: join(existing, 'shellcheck') };
		await publishTools(tools, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		await publishTools(tools, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		await rm(step, { recursive: true });
		const paths = (await readFile(pathFile, 'utf8')).trim().split('\n');
		assert.equal(paths.length, 4);
		assert.equal(paths[0], existing);
		assert.equal(paths[2], existing);
		assert.notEqual(paths[1], paths[3]);
		for (const publication of [paths.slice(0, 2), paths]) {
			const result = spawnSync(
				'actionlint',
				['-e', 'process.stdout.write("next step");'],
				{ cwd: job, env: withPath([...publication].reverse()), encoding: 'utf8', windowsHide: true },
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
		await publishTools({ shellcheck }, { GITHUB_PATH: pathFile });
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

test('publishing no tools leaves PATH untouched and existing tools need no artifact directory', async () => {
	await temporary(async (job) => {
		const pathFile = join(job, 'github-path');
		await publishTools({}, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		assert.deepEqual(await readdir(job), []);
		const shellcheck = join(job, 'installed', 'shellcheck');
		await publishTools({ shellcheck }, { GITHUB_PATH: pathFile });
		assert.equal(await readFile(pathFile, 'utf8'), `${dirname(shellcheck)}\n`);
		assert.deepEqual(await readdir(job), ['github-path']);
	});
});
