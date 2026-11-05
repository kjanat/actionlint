import assert from 'node:assert/strict';
import { test } from 'node:test';

import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { access, copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { delimiter, isAbsolute, join, relative } from 'node:path';
import { promisify } from 'node:util';

import { capture, temporary, which } from '#native';
import { commandEscape, readOutputs, writeOutputs } from '#workflow';

test('temporary cleanup retries a running Windows executable until it closes', {
	skip: process.platform !== 'win32',
}, async () => {
	let directory = '';
	let closed: Promise<unknown> | undefined;
	try {
		await temporary(async (path) => {
			directory = path;
			const executable = join(path, 'locked executable.exe');
			await copyFile(process.execPath, executable);
			const child = spawn(executable, ['-e', 'process.stdout.write("ready"); setTimeout(() => {}, 250)'], {
				stdio: ['ignore', 'pipe', 'ignore'],
				windowsHide: true,
			});
			closed = once(child, 'close');
			await Promise.race([
				once(child.stdout, 'data'),
				closed.then(() => {
					throw new Error('Executable exited before acquiring its cleanup lock');
				}),
			]);
			// Enter cleanup while Windows still holds the executable open.
		});
		await assert.rejects(access(directory), { code: 'ENOENT' });
	} finally {
		await closed;
	}
});

test('configuration preflight can terminate a noncooperative child at its deadline', async () => {
	await assert.rejects(
		capture(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], process.env, { timeoutMS: 50 }),
		/timed out after 50ms/,
	);
});

for (const wrapperExits of [false, true]) {
	test(`capture deadline releases inherited pipes and the caller can exit (wrapper exits: ${wrapperExits})`, {
		// Windows does not keep these inherited anonymous pipes open after the wrapper exits.
		skip: process.platform === 'win32',
	}, async () => {
		await temporary(async (directory) => {
			const pidFile = join(directory, 'descendant.pid');
			const stopDescendant = async () => {
				const recorded = await readFile(pidFile, 'utf8').catch(() => '');
				assert.ok(recorded !== '', 'the wrapper did not start its descendant before the capture deadline');
				const pid = Number(recorded);
				assert.ok(Number.isSafeInteger(pid) && pid > 0);
				try {
					process.kill(pid, 'SIGKILL');
				} catch (error) {
					if (!(error instanceof Error) || !('code' in error) || error.code !== 'ESRCH') throw error;
				}
			};
			const wrapper = `
const { spawn } = require('node:child_process');
const { writeFileSync } = require('node:fs');
const descendant = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 20000)'], {
	stdio: ['ignore', 'inherit', 'inherit'], windowsHide: true,
});
writeFileSync(${JSON.stringify(pidFile)}, String(descendant.pid));
${wrapperExits ? 'descendant.unref();' : 'setInterval(() => {}, 1000);'}
`;
			const caller = `
import { capture } from ${JSON.stringify(import.meta.resolve('#native'))};
try {
	await capture(process.execPath, ['-e', ${JSON.stringify(wrapper)}], process.env, { timeoutMS: 3000 });
	process.exitCode = 1;
} catch (error) {
	console.log(error.message);
}
`;
			try {
				const result = await promisify(execFile)(process.execPath, ['--input-type=module', '-e', caller], {
					timeout: 10_000,
					killSignal: 'SIGKILL',
					windowsHide: true,
				});
				assert.match(result.stdout, /timed out after 3000ms/);
			} finally {
				await stopDescendant();
			}
		});
	});
}

test('PATH lookup resolves relative entries before the working directory can change', async () => {
	await temporary(async (directory) => {
		const tools = join(directory, 'tools with spaces');
		await mkdir(tools);
		const executable = join(tools, process.platform === 'win32' ? 'probe.exe' : 'probe');
		await writeFile(executable, 'executable', { mode: 0o755 });
		const found = await which('probe', { PATH: `"${relative(process.cwd(), tools)}"` });
		assert.equal(found, executable);
		assert.ok(isAbsolute(found));
	});
});

test('explicit relative executable paths bypass PATH lookup', async () => {
	await temporary(async (directory) => {
		const pathDirectory = join(directory, 'path tools');
		await mkdir(pathDirectory);
		const name = process.platform === 'win32' ? 'probe.exe' : 'probe';
		for (const base of [directory, pathDirectory]) {
			await writeFile(join(base, name), 'executable', { mode: 0o755 });
		}
		const previous = process.cwd();
		try {
			process.chdir(directory);
			const environment = { PATH: pathDirectory };
			assert.equal(await which(name, environment), join(pathDirectory, name));
			assert.equal(await which(`./${name}`, environment), join(directory, name));
			if (process.platform === 'win32') {
				assert.equal(await which(`.\\${name}`, environment), join(directory, name));
			} else {
				const literalBackslash = 'probe\\name';
				await writeFile(join(pathDirectory, literalBackslash), 'executable', { mode: 0o755 });
				assert.equal(await which(literalBackslash, environment), join(pathDirectory, literalBackslash));
			}
		} finally {
			process.chdir(previous);
		}
	});
});

test('empty PATH components preserve current-directory lookup and search order', async () => {
	await temporary(async (directory) => {
		const tools = join(directory, 'tools');
		const missing = join(directory, 'missing');
		await mkdir(tools);
		const name = process.platform === 'win32' ? 'probe.exe' : 'probe';
		for (const base of [directory, tools]) {
			await writeFile(join(base, name), 'executable', { mode: 0o755 });
		}
		const previous = process.cwd();
		try {
			process.chdir(directory);
			for (const entries of [['', tools], [missing, '', tools], [missing, ''], ['']]) {
				assert.equal(await which(name, { PATH: entries.join(delimiter) }), join(directory, name));
			}
			assert.equal(await which(name, { PATH: [tools, ''].join(delimiter) }), join(tools, name));
			assert.equal(await which(name, {}), '');
		} finally {
			process.chdir(previous);
		}
	});
});

test('Windows lookup follows PATH and PATHEXT order, with native-only selection past batch launchers', {
	skip: process.platform !== 'win32',
}, async () => {
	await temporary(async (directory) => {
		const first = join(directory, 'first tools');
		const second = join(directory, 'second tools');
		await mkdir(first);
		await mkdir(second);
		for (const path of [join(first, 'probe.cmd'), join(first, 'probe.cmd.com'), join(second, 'probe.exe')]) {
			await writeFile(path, 'tool');
		}
		await copyFile(process.execPath, join(first, 'probe.com'));
		const environment = { Path: `"${first}";${second}`, PathExt: '.CMD;.COM;.EXE' };
		assert.equal(await which('probe', environment), join(first, 'probe.cmd'));
		assert.equal(await which('probe', environment, 'native'), join(first, 'probe.com'));
		assert.equal(await which('probe', { ...environment, PathExt: '.EXE;.CMD' }, 'native'), join(second, 'probe.exe'));
		assert.equal(await which('probe.com', environment), join(first, 'probe.com'));
		assert.equal(await which('probe.cmd', environment, 'native'), '');
		assert.equal(await which('probe', { ...environment, PathExt: '.BAT' }), '');
		assert.equal(await which('probe', { ...environment, PathExt: '.cOm;.cMd;.CoM' }), join(first, 'probe.com'));
		const native = await which('probe', environment, 'native');
		const result = await capture(native, ['-e', 'process.stdout.write(process.argv[1])', 'argument with spaces']);
		assert.equal(result.exitCode, 0, result.stderr);
		assert.equal(result.stdout, 'argument with spaces');
	});
});

test('workflow outputs frame multiline values and commands escape control characters', async () => {
	await temporary(async (directory) => {
		const output = join(directory, 'output');
		await writeOutputs(output, { 'exit-code': '3', output: 'first\n::error::literal\nlast', result: '' });
		const content = await readFile(output, 'utf8');
		const fields = [...content.matchAll(/([a-z-]+)<<([^\n]+)\n([\s\S]*?)\n\2\n/g)];
		assert.deepEqual(fields.map((field) => [field[1], field[3]]), [
			['exit-code', '3'],
			['output', 'first\n::error::literal\nlast'],
			['result', ''],
		]);
		assert.equal(commandEscape('a%\r\n::error::b'), 'a%25%0D%0A::error::b');
		assert.deepEqual(await readOutputs(output), {
			'exit-code': '3',
			output: 'first\n::error::literal\nlast',
			result: '',
		});
		await writeFile(output, 'output<<missing\npartial\n');
		await assert.rejects(readOutputs(output), /Unterminated/);
	});
});
