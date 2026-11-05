import assert from 'node:assert/strict';
import { test } from 'node:test';

import { spawnSync } from 'node:child_process';
import { copyFile, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { delimiter, join } from 'node:path';

import { temporary } from '#native';
import { publishActionlint } from '#path';

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
		const executable = binary;
		await publishActionlint(executable, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
		await publishActionlint(executable, { RUNNER_TEMP: job, GITHUB_PATH: pathFile });
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
			assert.equal(result.status, 0, result.stderr || result.error?.message || 'actionlint failed');
			assert.equal(result.stdout, 'next step');
		}
	});
});

test('failed actionlint publication cleans its artifacts', async () => {
	await temporary(async (job) => {
		const pathFile = join(job, 'github-path');
		assert.deepEqual(await readdir(job), []);
		await assert.rejects(
			publishActionlint(process.execPath, { GITHUB_PATH: pathFile }),
			/RUNNER_TEMP/,
		);
		await assert.rejects(
			publishActionlint(process.execPath, {
				RUNNER_TEMP: job,
				GITHUB_PATH: join(job, 'missing directory', 'github-path'),
			}),
			/ENOENT/,
		);
		assert.deepEqual(await readdir(job), []);
	});
});
