import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, isAbsolute, join, relative } from 'node:path';

import { cacheTool, capture, findTool, temporary, which } from '#native';
import { commandEscape, writeOutputs } from '#workflow';

test('configuration preflight can terminate a noncooperative child at its deadline', async () => {
	await assert.rejects(
		capture(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], process.env, 50),
		/terminated by SIGKILL/,
	);
});

test('external tool cache reuses copied files and ignores directories without completion markers', async () => {
	await temporary(async (directory) => {
		const previous = process.env.RUNNER_TOOL_CACHE;
		process.env.RUNNER_TOOL_CACHE = join(directory, 'cache');
		try {
			assert.equal(await findTool('shellcheck', '1.2.3', 'amd64'), '');
			const source = join(directory, 'source');
			await mkdir(source);
			await writeFile(join(source, 'shellcheck'), 'verified executable');
			const cached = await cacheTool(source, 'shellcheck', '1.2.3', 'amd64');
			assert.equal(await findTool('shellcheck', '1.2.3', 'amd64'), cached);
			assert.equal(await readFile(join(cached, 'shellcheck'), 'utf8'), 'verified executable');
			await mkdir(join(directory, 'cache', 'shellcheck', 'incomplete', 'amd64'), { recursive: true });
			assert.equal(await findTool('shellcheck', 'incomplete', 'amd64'), '');
		} finally {
			if (previous === undefined) delete process.env.RUNNER_TOOL_CACHE;
			else process.env.RUNNER_TOOL_CACHE = previous;
		}
	});
});

test('external tool cache recovers around interrupted canonical and generation directories', async () => {
	await temporary(async (directory) => {
		const previous = process.env.RUNNER_TOOL_CACHE;
		process.env.RUNNER_TOOL_CACHE = join(directory, 'cache');
		try {
			const canonical = join(directory, 'cache', 'shellcheck', '1.2.3', 'amd64');
			for (const interrupted of [canonical, `${canonical}-interrupted`]) {
				await mkdir(interrupted, { recursive: true });
				await writeFile(join(interrupted, 'shellcheck'), 'incomplete executable');
			}
			assert.equal(await findTool('shellcheck', '1.2.3', 'amd64'), '');
			const source = join(directory, 'source');
			await mkdir(source);
			await writeFile(join(source, 'shellcheck'), 'verified executable');
			const cached = await cacheTool(source, 'shellcheck', '1.2.3', 'amd64');
			assert.equal(await findTool('shellcheck', '1.2.3', 'amd64'), cached);
			assert.equal(await readFile(join(cached, 'shellcheck'), 'utf8'), 'verified executable');
			assert.equal(await cacheTool(source, 'shellcheck', '1.2.3', 'amd64'), cached);
			// An incomplete directory may belong to a live installer and must remain untouched.
			for (const interrupted of [canonical, `${canonical}-interrupted`]) {
				assert.equal(await readFile(join(interrupted, 'shellcheck'), 'utf8'), 'incomplete executable');
			}
		} finally {
			if (previous === undefined) delete process.env.RUNNER_TOOL_CACHE;
			else process.env.RUNNER_TOOL_CACHE = previous;
		}
	});
});

test('concurrent tool installers publish complete generations without replacing each other', async () => {
	await temporary(async (directory) => {
		const previous = process.env.RUNNER_TOOL_CACHE;
		process.env.RUNNER_TOOL_CACHE = join(directory, 'cache');
		try {
			const sources = await Promise.all(Array.from({ length: 4 }, async (_, index) => {
				const source = join(directory, `source-${index}`);
				await mkdir(source);
				await writeFile(join(source, 'executable'), String(index));
				await writeFile(join(source, 'metadata'), String(index));
				return source;
			}));
			const installations = await Promise.all(sources.map((source) => cacheTool(source, 'shellcheck', '1.2.3', 'amd64')));
			for (const installation of installations) {
				const executable = await readFile(join(installation, 'executable'), 'utf8');
				assert.match(executable, /^[0-3]$/);
				assert.equal(await readFile(join(installation, 'metadata'), 'utf8'), executable);
				assert.equal(await readFile(`${installation}.complete`, 'utf8'), '');
			}
			const selected = [...new Set(installations)].sort()[0];
			assert.equal(await findTool('shellcheck', '1.2.3', 'amd64'), selected);
			assert.equal(await cacheTool('unused source', 'shellcheck', '1.2.3', 'amd64'), selected);
			for (const installation of installations) {
				assert.match(await readFile(join(installation, 'executable'), 'utf8'), /^[0-3]$/);
			}
		} finally {
			if (previous === undefined) delete process.env.RUNNER_TOOL_CACHE;
			else process.env.RUNNER_TOOL_CACHE = previous;
		}
	});
});

test('legacy completed tool caches remain preferred over completed generations', async () => {
	await temporary(async (directory) => {
		const previous = process.env.RUNNER_TOOL_CACHE;
		process.env.RUNNER_TOOL_CACHE = join(directory, 'cache');
		try {
			const canonical = join(directory, 'cache', 'shellcheck', '1.2.3', 'amd64');
			await mkdir(dirname(canonical), { recursive: true });
			for (const installation of [canonical, `${canonical}-other`]) {
				await mkdir(installation);
				await writeFile(`${installation}.complete`, '');
			}
			assert.equal(await findTool('shellcheck', '1.2.3', 'amd64'), canonical);
			assert.equal(await cacheTool('unused source', 'shellcheck', '1.2.3', 'amd64'), canonical);
		} finally {
			if (previous === undefined) delete process.env.RUNNER_TOOL_CACHE;
			else process.env.RUNNER_TOOL_CACHE = previous;
		}
	});
});

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
	});
});
