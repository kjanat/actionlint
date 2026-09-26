import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { capture } from '#native';
import { annotation, report, reportOptions, summary, withReporting } from '#reporters';
import type { ActionResult, Diagnostic } from '#result';
import { parseResult } from '#result';

const diagnostic: Diagnostic = {
	rule: 'shellcheck',
	code: 'SC2086',
	severity: 'warning',
	message: 'Quote this & <value>\n::warning::spoof',
	path: 'a,b.yml',
	start: { line: 2, column: 3 },
	end: { line: 2, column: 8 },
	snippet: 'echo "$foo"',
};
const result: ActionResult = {
	schema_version: 1,
	completed: true,
	status: 'problems-found',
	exit_code: 1,
	file_count: 2,
	diagnostics: [diagnostic],
	configurations: [],
	hints: [],
	sarif: { version: '2.1.0', runs: [{ results: [{ message: { text: diagnostic.message } }] }] },
};

test('persisted results validate completion and preserve diagnostic severity and fixes', () => {
	assert.deepEqual(parseResult(result), result);
	assert.throws(() => parseResult({ ...result, completed: false }));
	assert.throws(() => parseResult({ ...result, schema_version: 2 }));
	assert.throws(() => parseResult({ ...result, diagnostics: [{ ...diagnostic, start: { line: -1, column: 3 } }] }));
	assert.throws(() => parseResult({ ...result, status: 'success', exit_code: 0 }));
});

test('report controls are independent and reject unsupported inputs', () => {
	assert.deepEqual(reportOptions({}), { annotations: 'auto', summary: true, sarif: false, review: false });
	assert.deepEqual(reportOptions({ INPUT_SARIF: 'true', INPUT_SUMMARY: 'true' }), {
		annotations: 'auto',
		summary: true,
		sarif: true,
		review: false,
	});
	assert.throws(() => reportOptions({ INPUT_REVIEW: 'maybe' }));
	assert.throws(() => reportOptions({ INPUT_SARIF: 'xml' }));
	assert.throws(() => reportOptions({ INPUT_ANNOTATIONS: 'maybe' }));
	assert.equal(reportOptions({ INPUT_ANNOTATIONS: 'false', INPUT_SUMMARY: 'false' }).summary, false);
});

test('summaries distinguish clean analysis, no selection, and incomplete analysis', () => {
	const clean: ActionResult = { ...result, status: 'success', exit_code: 0, diagnostics: [] };
	assert.equal(summary(clean), '### actionlint: No findings in 2 workflows\n\n');
	assert.equal(summary({ ...clean, file_count: 0 }), '### actionlint: No workflows selected\n\n');
	const incomplete: ActionResult = { ...result, completed: false, status: 'failure', exit_code: 3 };
	assert.ok(summary(incomplete).startsWith('### actionlint: Analysis incomplete\n\nExit 3; 1 finding collected.'));
	assert.ok(
		summary({ ...result, diagnostics: Array.from({ length: 51 }, () => diagnostic) }).includes('first 50 findings'),
	);
});

test('configuration warnings and origins survive parsing without becoming required', () => {
	const configuration = { file: '.github/actionlint.yaml', project: '.', overrides: null };
	for (
		const config of [configuration, {
			...configuration,
			overrides: ['config'],
			origins: { tools: { source: 'file', state: 'set', line: 2, column: 1 } },
			warnings: [{ message: 'unknown key', line: 1, column: 1 }],
		}]
	) {
		assert.deepEqual(parseResult({ ...result, configurations: [config] }).configurations, [config]);
	}
	assert.throws(() => parseResult({ ...result, configurations: [{ ...configuration, warnings: ['wrong shape'] }] }));
});

test('annotation overrides add commands only for explicit true with a non-GitHub format', async () => {
	for (const format of ['', 'github', 'json', 'oneline']) {
		for (const annotations of ['auto', 'true', 'false']) {
			const messages: string[] = [];
			const environment = { INPUT_FORMAT: format, INPUT_ANNOTATIONS: annotations, INPUT_SUMMARY: 'false' };
			await report(result, 'result.json', environment, reportOptions(environment), {
				log: (message) => messages.push(message),
				review: async () => assert.fail('review disabled'),
			});
			assert.equal(messages.length, annotations === 'true' && format !== '' && format !== 'github' ? 1 : 0);
		}
	}
});

test('reporter failures preserve native result and let other destinations finish', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-report-failure-'));
	try {
		const output = join(directory, 'outputs');
		const stepSummary = join(directory, 'summary');
		const messages: string[] = [];
		let resultPath = '';
		let reviews = 0;
		const code = await withReporting({
			RUNNER_TEMP: directory,
			GITHUB_OUTPUT: output,
			GITHUB_STEP_SUMMARY: stepSummary,
			INPUT_SARIF: 'true',
			INPUT_REVIEW: 'true',
		}, async (environment) => {
			assert.ok(environment.ACTIONLINT_ACTION_RESULT);
			resultPath = environment.ACTIONLINT_ACTION_RESULT;
			await writeFile(resultPath, JSON.stringify(result));
			await mkdir(`${resultPath}.sarif`);
			return 1;
		}, {
			log: (text) => messages.push(text),
			review: async () => {
				reviews++;
				return 'PR review: 1 comment posted.';
			},
		});
		assert.equal(code, 1);
		assert.deepEqual(parseResult(JSON.parse(await readFile(resultPath, 'utf8'))), result);
		const outputs = await readFile(output, 'utf8');
		assert.ok(outputs.includes(`\n${resultPath}\n`));
		assert.ok(!outputs.includes('analysis-result') && !outputs.includes('report-json'));
		assert.ok((await readFile(stepSummary, 'utf8')).includes('SC2086'));
		assert.equal(reviews, 1);
		assert.ok(messages.some((message) => message.startsWith('::warning::SARIF report unavailable:')));
		await report(
			result,
			resultPath,
			{ GITHUB_STEP_SUMMARY: directory, INPUT_REVIEW: 'true' },
			reportOptions({ INPUT_REVIEW: 'true' }),
			{
				log: (text) => messages.push(text),
				review: async () => {
					reviews++;
					return 'PR review skipped: no pull request context.';
				},
			},
		);
		assert.equal(reviews, 2);
		assert.ok(messages.some((message) => message.startsWith('::warning::Job summary unavailable:')));
	} finally {
		await rm(directory, { recursive: true });
	}
});

test('native process failures preserve collected diagnostics and configuration', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-partial-test-'));
	try {
		for (const reject of [false, true]) {
			let resultPath = '';
			const outputPath = join(directory, `outputs-${reject}`);
			await assert.rejects(
				withReporting(
					{ RUNNER_TEMP: directory, GITHUB_OUTPUT: outputPath, INPUT_SARIF: 'true' },
					async (environment) => {
						assert.ok(environment.ACTIONLINT_ACTION_RESULT);
						resultPath = environment.ACTIONLINT_ACTION_RESULT;
						await writeFile(resultPath, JSON.stringify(result));
						if (reject) throw new Error('native process crashed');
						return 3;
					},
				),
			);
			const persisted = parseResult(JSON.parse(await readFile(resultPath, 'utf8')));
			assert.equal(persisted.completed, false);
			assert.equal(persisted.status, 'failure');
			assert.deepEqual(persisted.diagnostics, result.diagnostics);
			assert.equal(persisted.file_count, 2);
			assert.deepEqual(persisted.sarif, result.sarif);
			assert.match(await readFile(outputPath, 'utf8'), /report-sarif<<[^\n]+\n\n/);
			await assert.rejects(readFile(`${resultPath}.sarif`), { code: 'ENOENT' });
		}
	} finally {
		await rm(directory, { recursive: true });
	}
});

test('real entrypoint rejects malformed native results and summarizes input failures', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-main-failure-'));
	try {
		// A native-process stub returns unusable data to the real entrypoint and reporters.
		const launcher = join(directory, 'main-fixture.mjs');
		const toolsURL = new URL('../src/tools.ts', import.meta.url).href;
		const workflowURL = new URL('../src/workflow.ts', import.meta.url).href;
		const mainURL = new URL('../src/main.ts', import.meta.url).href;
		await writeFile(
			launcher,
			[
				"import assert from 'node:assert/strict';",
				"import { writeFile } from 'node:fs/promises';",
				"import { mock } from 'node:test';",
				`import * as tools from ${JSON.stringify(toolsURL)};`,
				`import { writeOutputs } from ${JSON.stringify(workflowURL)};`,
				`mock.module(${JSON.stringify(toolsURL)}, { namedExports: { ...tools,`,
				'  inspectTools: async () => ({ shellcheck: false, pyflakes: false }),',
				'  executeNative: async (_executable, _args, environment) => {',
				'    assert.ok(environment.ACTIONLINT_ACTION_RESULT);',
				'    assert.ok(environment.ACTIONLINT_TEST_RESULT);',
				'    await writeFile(environment.ACTIONLINT_ACTION_RESULT, environment.ACTIONLINT_TEST_RESULT);',
				"    await writeOutputs(environment.GITHUB_OUTPUT, { result: 'success', 'exit-code': '0' });",
				'    return 0;',
				'  },',
				'} });',
				`await import(${JSON.stringify(mainURL)});`,
			].join('\n'),
		);
		const scenarios = [
			{ name: 'invalid-json', data: '{"schema_version":', annotations: 'auto', code: 3, status: 'failure' },
			{ name: 'invalid-schema', data: '{"schema_version":2}', annotations: 'auto', code: 3, status: 'failure' },
			{ name: 'invalid-input', data: '{}', annotations: 'wrong', code: 2, status: 'invalid-options' },
		];
		for (const scenario of scenarios) {
			const outputPath = join(directory, `${scenario.name}-outputs`);
			const summaryPath = join(directory, `${scenario.name}-summary`);
			const child = await capture(process.execPath, ['--experimental-test-module-mocks', launcher], {
				RUNNER_TEMP: directory,
				GITHUB_OUTPUT: outputPath,
				GITHUB_STEP_SUMMARY: summaryPath,
				ACTIONLINT_ACTION_BINARY: process.execPath,
				ACTIONLINT_TEST_RESULT: scenario.data,
				INPUT_ANNOTATIONS: scenario.annotations,
				'INPUT_ADD-ACTIONLINT-TO-PATH': 'false',
				'INPUT_ADD-SHELLCHECK-TO-PATH': 'false',
				'INPUT_ADD-PYFLAKES-TO-PATH': 'false',
			}, { timeoutMS: 5_000 });
			assert.equal(child.exitCode, scenario.code, child.stderr);
			assert.ok(child.stdout.includes('::error::'));
			const outputs = await readFile(outputPath, 'utf8');
			const statuses = [...outputs.matchAll(/^result<<[^\n]+\n([^\n]+)\n/gm)].map((match) => match[1]);
			assert.deepEqual(statuses, scenario.code === 2 ? [scenario.status] : ['success', scenario.status]);
			const resultPath = /^result-file<<[^\n]+\n([^\n]+)\n/m.exec(outputs)?.[1];
			assert.ok(resultPath);
			const persisted = parseResult(JSON.parse(await readFile(resultPath, 'utf8')));
			assert.equal(persisted.completed, false);
			assert.equal(persisted.status, scenario.status);
			assert.equal(persisted.exit_code, scenario.code);
			assert.ok((await readFile(summaryPath, 'utf8')).startsWith('### actionlint: Analysis incomplete'));
		}
	} finally {
		await rm(directory, { recursive: true });
	}
});

test('annotation commands and Markdown summaries escape diagnostic content', () => {
	assert.equal(
		annotation(diagnostic),
		'::warning file=a%2Cb.yml,line=2,endLine=2,col=3,endColumn=7,title=SC2086::Quote this & <value>%0A::warning::spoof',
	);
	const text = summary(result);
	assert.ok(text.includes('&amp; &lt;value&gt;'));
	assert.ok(text.includes('<p>Quote this &amp; &lt;value&gt;<br>::warning::spoof</p>'));
	assert.ok(!text.includes('<value>'));
	assert.ok(text.includes('1 finding in 2 workflows'));
	const multiline = annotation({ ...diagnostic, end: { line: 4, column: 1 } });
	assert.ok(multiline.includes('line=2,endLine=3,title='));
	assert.ok(!multiline.includes(',col='));
});

test('one analysis feeds reports; github annotations are not emitted twice; review errors are advisory', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-report-test-'));
	try {
		const path = join(directory, 'result.json');
		const output = join(directory, 'outputs');
		const stepSummary = join(directory, 'summary');
		const messages: string[] = [];
		const environment = { GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: stepSummary, INPUT_FORMAT: 'github' };
		await report(
			result,
			path,
			environment,
			{ annotations: 'true', summary: true, sarif: true, review: true },
			{
				log: (text) => messages.push(text),
				review: async () => {
					throw new Error('HTTP 403');
				},
			},
		);
		assert.equal(messages.length, 1);
		assert.ok(messages[0]?.startsWith('::warning::PR review skipped: HTTP 403'));
		assert.deepEqual(JSON.parse(await readFile(`${path}.sarif`, 'utf8')), result.sarif);
		assert.ok((await readFile(stepSummary, 'utf8')).includes('SC2086'));
		assert.ok((await readFile(output, 'utf8')).includes('result-file<<'));
		await report(result, path, { INPUT_FORMAT: 'json' }, {
			annotations: 'true',
			summary: false,
			sarif: false,
			review: false,
		}, {
			log: (text) => messages.push(text),
			review: async () => {
				assert.fail('review disabled');
			},
		});
		assert.equal(messages.filter((text) => text.startsWith('::warning file=')).length, 1);
	} finally {
		await rm(directory, { recursive: true });
	}
});

test('unique results survive for later steps and preserve fail-on-error false', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-persist-test-'));
	try {
		const paths: string[] = [];
		for (let invocation = 0; invocation < 2; invocation++) {
			assert.equal(
				await withReporting({ RUNNER_TEMP: directory, 'INPUT_FAIL-ON-ERROR': 'false' }, async (environment) => {
					const path = environment.ACTIONLINT_ACTION_RESULT;
					assert.ok(path);
					paths.push(path);
					await writeFile(path, JSON.stringify(result));
					return 0;
				}),
				0,
			);
		}
		assert.notEqual(paths[0], paths[1]);
		for (const path of paths) assert.deepEqual(parseResult(JSON.parse(await readFile(path, 'utf8'))), result);
	} finally {
		await rm(directory, { recursive: true });
	}
});

test('annotations rebase working-directory paths to the workspace without changing persisted diagnostics', async () => {
	const workspace = join(tmpdir(), 'actionlint-annotation-workspace');
	for (
		const example of [
			{ directory: '.', path: 'a,b.yml', expected: 'a%2Cb.yml' },
			{ directory: 'packages/service', path: 'a,b.yml', expected: 'packages/service/a%2Cb.yml' },
			{ directory: 'packages/service', path: '../shared/ci.yml', expected: 'packages/shared/ci.yml' },
			{ directory: 'packages/service', path: join(workspace, '.github', 'ci.yml'), expected: '.github/ci.yml' },
		]
	) {
		const finding = { ...diagnostic, path: example.path };
		const persisted: ActionResult = { ...result, diagnostics: [finding] };
		const messages: string[] = [];
		await report(
			persisted,
			'unused-result.json',
			{
				GITHUB_WORKSPACE: workspace,
				'INPUT_WORKING-DIRECTORY': example.directory,
				INPUT_FORMAT: 'json',
			},
			{ annotations: 'true', summary: false, sarif: false, review: false },
			{
				log: (text) => messages.push(text),
				review: async () => {
					assert.fail('review disabled');
				},
			},
		);
		assert.equal(messages.length, 1);
		const message = messages[0];
		assert.ok(message);
		assert.ok(
			message.startsWith(`::warning file=${example.expected},line=2,endLine=2,col=3,endColumn=7,title=SC2086::`),
			message,
		);
		assert.equal(persisted.diagnostics[0]?.path, example.path);
	}
});

test('missing or corrupt native results cannot become a clean analysis', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-incomplete-test-'));
	try {
		const disabledSummary = join(directory, 'disabled-summary');
		await assert.rejects(
			withReporting({
				RUNNER_TEMP: directory,
				INPUT_ANNOTATIONS: 'invalid',
				INPUT_SUMMARY: 'false',
				GITHUB_STEP_SUMMARY: disabledSummary,
			}, async () => assert.fail('invalid inputs must stop before execution')),
			/Input 'annotations'/,
		);
		await assert.rejects(readFile(disabledSummary), { code: 'ENOENT' });
		await assert.rejects(withReporting({ RUNNER_TEMP: directory }, async () => 0), /did not write a completed result/);
		await assert.rejects(withReporting({ RUNNER_TEMP: directory }, async (environment) => {
			const path = environment.ACTIONLINT_ACTION_RESULT;
			assert.ok(path);
			await writeFile(path, '{"schema_version":');
			return 0;
		}));
		const output = join(directory, 'outputs');
		await assert.rejects(
			withReporting({ RUNNER_TEMP: directory }, async (environment) => {
				const path = environment.ACTIONLINT_ACTION_RESULT;
				assert.ok(path);
				await writeFile(path, JSON.stringify(result));
				return 3;
			}),
			/process status disagrees/,
		);
		await assert.rejects(
			withReporting({ RUNNER_TEMP: directory, GITHUB_OUTPUT: output }, async () => {
				throw new Error('download failed');
			}),
			/download failed/,
		);
		assert.ok((await readFile(output, 'utf8')).includes('result-file<<'));
	} finally {
		await rm(directory, { recursive: true });
	}
});
