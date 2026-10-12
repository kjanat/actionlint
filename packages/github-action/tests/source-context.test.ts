import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { report } from '#reporters';
import type { ActionResult, Diagnostic } from '#result';
import { sourceContexts } from '#source-context';
import { summary } from '#summary';

test('summary reads bounded workspace context and keeps stale/external sources on snippet fallback', async () => {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-context-'));
	try {
		const workspace = join(root, 'workspace');
		await mkdir(join(workspace, 'sub'), { recursive: true });
		const lines = ['first', 'second', 'third', 'echo missing', 'fifth', 'sixth', 'seventh'];
		await writeFile(join(workspace, 'sub', 'ci.yml'), lines.join('\r\n'));
		await writeFile(join(root, 'outside.yml'), lines.join('\n'));
		await writeFile(join(workspace, 'large.yml'), 'x'.repeat(1024 * 1024 + 1));
		const finding: Diagnostic = {
			path: 'ci.yml',
			rule: 'expression',
			message: 'Missing variable',
			snippet: 'echo missing',
			start: { line: 4, column: 6 },
			end: { line: 4, column: 13 },
		};
		const diagnostics = [finding, { ...finding, path: '../../outside.yml' }, { ...finding, path: '../large.yml' }, {
			...finding,
			path: 'missing.yml',
		}, { ...finding, snippet: 'stale' }];
		if (process.platform !== 'win32') {
			await symlink(join(root, 'outside.yml'), join(workspace, 'sub', 'escape.yml'));
			diagnostics.push({ ...finding, path: 'escape.yml' });
		}
		const result: ActionResult = {
			schema_version: 1,
			status: 'problems-found',
			completed: true,
			exit_code: 1,
			file_count: 1,
			configurations: [],
			hints: [],
			diagnostics,
		};
		const environment = { GITHUB_WORKSPACE: workspace, 'INPUT_WORKING-DIRECTORY': 'sub' };
		const contexts = await sourceContexts(result, environment);
		assert.equal(contexts.size, 1);
		assert.deepEqual(contexts.get(finding), lines);
		const rendered = summary({ ...result, diagnostics: [finding] }, environment, contexts);
		assert.ok(
			rendered.includes(
				'2 | second&#10;3 | third&#10;4 | echo missing&#10;  |      ^~~~~~~&#10;5 | fifth&#10;6 | sixth',
			),
		);
		assert.ok(!rendered.includes('first'));
		assert.ok(!rendered.includes('seventh'));
		const summaryPath = join(root, 'summary');
		await report(
			{ ...result, diagnostics: [finding] },
			'result.json',
			{ ...environment, GITHUB_STEP_SUMMARY: summaryPath },
			{ summary: true, annotations: 'false', sarif: false, review: false },
			{ log: () => {}, review: async () => assert.fail('review disabled') },
		);
		assert.equal(await readFile(summaryPath, 'utf8'), rendered);
		for (const number of [1, 7]) {
			const edge = {
				...finding,
				snippet: lines[number - 1] || '',
				start: { line: number, column: 1 },
				end: { line: number + 1, column: 1 },
			};
			const edgeResult = { ...result, diagnostics: [edge] };
			const edgeContexts = await sourceContexts(edgeResult, environment);
			const edgeSummary = summary(edgeResult, environment, edgeContexts);
			assert.ok(edgeSummary.includes(`${number} | ${lines[number - 1]}&#10;  | ^`));
			assert.ok(!edgeSummary.includes('4 |'));
		}
		for (const newline of ['\n', '\r\n', '\r']) {
			for (const endings of [1, 2]) {
				await writeFile(join(workspace, 'sub', 'ci.yml'), lines.join(newline) + newline.repeat(endings));
				const eofContexts = await sourceContexts({ ...result, diagnostics: [finding] }, environment);
				assert.deepEqual(eofContexts.get(finding), endings === 1 ? lines : [...lines, '']);
			}
		}
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});
