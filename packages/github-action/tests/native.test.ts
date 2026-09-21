import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { cacheTool, findTool, temporary } from '#native';
import { commandEscape, writeOutputs } from '#workflow';

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
