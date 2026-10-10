import assert from 'node:assert/strict';
import { test } from 'node:test';

import { chmod, copyFile, link, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { runnerPlatform } from '#assets';
import { capture, temporary } from '#native';
import { executeNative, shellcheckBinary } from '#tools';

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
	await temporary(async (directory) => {
		const name = process.platform === 'win32' ? 'node executable.exe' : 'node \\ " executable';
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
	});
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
		for (const executable of [shellcheck]) await copyFile(process.execPath, executable);
		await withEnvironment({ PATH: directory, PATHEXT: '.EXE' }, async () => {
			const platform = runnerPlatform(process.platform, process.arch);
			assert.deepEqual(await shellcheckBinary(platform), { kind: 'existing', executable: shellcheck });
			assert.deepEqual(messages, [
				`::debug::ShellCheck: existing installation; version ${process.version}; ${shellcheck}`,
			]);
		});
	});
});

test('failed version probes retain the selected PATH executable', async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	await temporary(async (directory) => {
		const extension = process.platform === 'win32' ? '.exe' : '';
		for (const name of ['shellcheck']) {
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
		});
	});
	assert.equal(messages.length, 1);
	for (const message of messages) assert.match(message, /existing installation; version unavailable \(.+\);/);
});

test('PATH version probes read tool banners without exposing the review token', {
	skip: process.platform === 'win32',
}, async (t) => {
	const messages: string[] = [];
	t.mock.method(console, 'log', (message: string) => messages.push(message));
	await temporary(async (directory) => {
		const shellcheck = join(directory, 'shellcheck');
		const guard = '#!/bin/sh\n[ "$1" = "--version" ] || exit 90\n[ -z "$INPUT_TOKEN" ] || exit 91\n';
		await writeFile(shellcheck, `${guard}printf 'ShellCheck - shell script analysis tool\\nversion: 0.11.0\\n'\n`);
		await chmod(shellcheck, 0o755);
		await withEnvironment({ PATH: directory, INPUT_TOKEN: 'fixture-review-token' }, async () => {
			const platform = runnerPlatform(process.platform, process.arch);
			await shellcheckBinary(platform);
		});
		assert.deepEqual(messages, [
			`::debug::ShellCheck: existing installation; version 0.11.0; ${shellcheck}`,
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
