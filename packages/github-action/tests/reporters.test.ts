import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { capture } from '#native';
import { annotation, report, reportOptions, summary, withReporting } from '#reporters';
import type { ActionResult, Diagnostic } from '#result';
import { parseResult } from '#result';
import { readOutputs, writeOutputs } from '#workflow';

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

test('normalized JSON output also replaces the native report file', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-report-json-'));
	try {
		for (const format of ['json', 'json-lines']) {
			const output = join(directory, `${format}-outputs`);
			const reportPath = join(directory, `${format}-report`);
			const legacy = JSON.stringify([{ message: diagnostic.message }]);
			await withReporting({
				RUNNER_TEMP: directory,
				GITHUB_WORKSPACE: directory,
				GITHUB_OUTPUT: output,
				INPUT_FORMAT: format,
				INPUT_SUMMARY: 'false',
			}, async (environment) => {
				assert.ok(environment.ACTIONLINT_ACTION_RESULT);
				await writeFile(environment.ACTIONLINT_ACTION_RESULT, JSON.stringify(result));
				await writeFile(reportPath, legacy);
				await writeOutputs(environment.GITHUB_OUTPUT, { output: legacy, 'output-file': `${format}-report` });
				return 1;
			});
			const outputs = await readOutputs(output);
			const expected: string = format === 'json'
				? `${JSON.stringify(result)}\n`
				: `${JSON.stringify({ schema_version: 1, ...diagnostic })}\n`;
			assert.equal(outputs.output, expected);
			assert.equal(await readFile(reportPath, 'utf8'), expected);
		}
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('persisted results validate completion and preserve diagnostic severity and fixes', () => {
	assert.deepEqual(parseResult(result), result);
	assert.throws(() => parseResult({ ...result, completed: false }));
	assert.throws(() => parseResult({ ...result, schema_version: 2 }));
	assert.throws(() => parseResult({ ...result, diagnostics: [{ ...diagnostic, start: { line: -1, column: 3 } }] }));
	assert.throws(() => parseResult({ ...result, status: 'success', exit_code: 0 }));
});

test('document outlines preserve parse states, nested steps and compatible additions', () => {
	for (const parseStatus of ['complete', 'partial', 'failed']) {
		const documents = [{
			kind: 'workflow',
			path: '.github/workflows/test.yml',
			name: 'Test',
			parse_status: parseStatus,
			triggers: ['push', 'workflow_dispatch'],
			jobs: [{
				id: 'build',
				name: 'Build',
				start: { line: 4, column: 3 },
				needs: ['prepare'],
				steps: [
					{ id: 'checkout', kind: 'uses', uses: 'actions/checkout@v6' },
					{ kind: 'parallel', steps: [{ name: 'Compile', kind: 'run', start: { line: 8, column: 9 } }] },
					{ kind: 'future-kind', future_metadata: { enabled: true } },
				],
			}, { id: 'call', needs: [], uses: './.github/workflows/reusable.yml', steps: [] }],
		}];
		for (const completed of [true, false]) {
			const source: unknown = completed
				? { ...result, documents }
				: { ...result, documents, completed: false, status: 'failure', exit_code: 3 };
			assert.deepEqual(JSON.parse(JSON.stringify(parseResult(source))), source);
		}
	}
	assert.equal(parseResult(result).documents, undefined);
	assert.deepEqual(parseResult({ ...result, documents: [] }).documents, []);
});

test('document outlines reject malformed known fields', () => {
	const workflow = { kind: 'workflow', path: 'test.yml', parse_status: 'complete', triggers: [], jobs: [] };
	const job = { id: 'build', needs: [], steps: [] };
	for (
		const documents of [
			null,
			{},
			[{ ...workflow, path: null }],
			[{ ...workflow, name: 1 }],
			[{ ...workflow, parse_status: 'success' }],
			[{ ...workflow, triggers: null }],
			[{ ...workflow, triggers: [1] }],
			[{ ...workflow, jobs: null }],
			...[{ ...job, id: null }, { ...job, needs: null }, { ...job, needs: [1] }, { ...job, steps: null }, {
				...job,
				uses: false,
			}, { ...job, start: { line: 0, column: 1 } }].map((invalidJob) => [{ ...workflow, jobs: [invalidJob] }]),
			...[
				{},
				{ kind: 1 },
				{ kind: 'uses', uses: 1 },
				{ kind: 'run', id: false },
				{ kind: 'run', name: null },
				{ kind: 'run', start: { line: 1, column: -1 } },
				{ kind: 'parallel', steps: null },
				{ kind: 'parallel', steps: [{ kind: 'run', start: null }] },
			].map((step) => [{ ...workflow, jobs: [{ ...job, steps: [step] }] }]),
		]
	) {
		assert.throws(() => parseResult({ ...result, documents }), /invalid or unsupported persisted result/);
	}
});

test('action outlines retain supported runtimes and their declarations', () => {
	for (
		const runs of [
			{ kind: 'composite', steps: [{ kind: 'run', name: 'Build' }] },
			{ kind: 'javascript', using: 'node24', main: 'action.mjs', pre: 'pre.mjs', post_if: 'always()' },
			{ kind: 'docker', image: 'Dockerfile', entrypoint: 'entry.sh', args: ['--check'] },
			{ kind: 'plugin', plugin: 'GitHub.Runner.Plugins.Checkout.CheckoutTask' },
			{ kind: 'unknown', using: 'future-runtime' },
		]
	) {
		const documents = [{
			kind: 'action',
			path: 'action.yml',
			name: 'Test',
			description: 'An action',
			parse_status: 'partial',
			inputs: [{ id: 'target', required: true, default: '.', description: 'Target', start: { line: 4, column: 3 } }],
			outputs: [{ id: 'result', value: `\${{ steps.check.outputs.result }}`, description: 'Result' }],
			runs,
			future_metadata: { enabled: true },
		}];
		assert.deepEqual(parseResult({ ...result, documents }).documents, documents);
	}
});

test('document and runtime variants reject mixed shapes while allowing future fields', () => {
	const action = {
		kind: 'action',
		path: 'action.yml',
		parse_status: 'complete',
		inputs: [],
		outputs: [],
		runs: { kind: 'unknown' },
	};
	const workflow = { kind: 'workflow', path: 'test.yml', parse_status: 'complete', triggers: [], jobs: [] };
	for (
		const document of [
			{ ...action, jobs: [] },
			{ ...action, triggers: [] },
			{ ...workflow, inputs: [] },
			{ ...workflow, outputs: [] },
			{ ...workflow, runs: { kind: 'unknown' } },
			{ ...workflow, description: 'Action-only field' },
			{ ...action, inputs: null },
			{ ...action, outputs: null },
			{ ...action, inputs: [{ id: 'input', required: 'true' }] },
			{ ...action, outputs: [{ id: 'result', value: 1 }] },
			...[
				{ kind: 'composite', steps: [], image: 'Dockerfile' },
				{ kind: 'javascript', using: 'node24', steps: [] },
				{ kind: 'javascript', using: 24 },
				{ kind: 'docker', args: [], main: 'action.mjs' },
				{ kind: 'docker', args: null },
				{ kind: 'plugin', plugin: 'plugin', using: 'node24' },
				{ kind: 'unknown', main: 'action.mjs' },
				{ kind: 'unknown', using: 1 },
			].map((runs) => ({ ...action, runs })),
		]
	) assert.throws(() => parseResult({ ...result, documents: [document] }));
});

test('uses references preserve parsed identity without inferring a provider', () => {
	const references = [
		{ kind: 'repository', owner: 'actions', repo: 'checkout', subpath: '', ref: 'v6', host_source: 'default' },
		{
			kind: 'repository',
			owner: 'owner',
			repo: 'repo',
			subpath: 'action',
			ref: 'main',
			host: 'git.example.com',
			scheme: 'https',
			host_source: 'explicit',
		},
		{ kind: 'repository', owner: 'owner', repo: 'repo', subpath: '', ref: 'main', host_source: 'self' },
		{ kind: 'workspace', path: './action' },
		{ kind: 'self-repository', path: '' },
		{ kind: 'container', image: 'alpine:latest' },
		{ kind: 'builtin', name: 'actions/checkout' },
		{ kind: 'unknown' },
	];
	const withReference = (reference: unknown) => ({
		...result,
		documents: [{
			kind: 'workflow',
			path: 'test.yml',
			parse_status: 'complete',
			triggers: [],
			jobs: [{ id: 'build', needs: [], reference, steps: [{ kind: 'uses', uses: 'original text', reference }] }],
		}],
	});
	for (const reference of references) {
		const source = withReference(reference);
		assert.deepEqual(parseResult(source).documents, source.documents);
	}
	for (
		const reference of [
			{ kind: 'repository', owner: 'owner', repo: 'repo', subpath: '', ref: 'main' },
			{ kind: 'repository', owner: 'owner', repo: 'repo', subpath: '', ref: 'main', host_source: 'guessed' },
			{ kind: 'workspace', path: 1 },
			{ kind: 'workspace', path: '.', host: 'github.com' },
			{ kind: 'container', image: 'alpine', ref: 'main' },
			{ kind: 'builtin', name: null },
			{ kind: 'unknown', path: '.' },
		]
	) assert.throws(() => parseResult(withReference(reference)));
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
		}, {
			...configuration,
			origins: { tools: { source: 'config', state: 'value', file: 'base.yml', line: 2, column: 1 } },
			warnings: [{ message: 'unknown key', file: 'base.yml', line: 3, column: 1 }],
		}]
	) {
		assert.deepEqual(parseResult({ ...result, configurations: [config] }).configurations, [config]);
	}
	assert.throws(() => parseResult({ ...result, configurations: [{ ...configuration, warnings: ['wrong shape'] }] }));
	assert.throws(() =>
		parseResult({
			...result,
			configurations: [{ ...configuration, warnings: [{ message: 'unknown key', file: 42, line: 1, column: 1 }] }],
		})
	);
	assert.throws(() =>
		parseResult({
			...result,
			configurations: [{ ...configuration, origins: { tools: { source: 'config', state: 'value', file: 42 } } }],
		})
	);
});

test('annotation policy is independent of serialization with auto enabled for GitHub format', async () => {
	for (const format of ['', 'github', 'json', 'oneline']) {
		for (const annotations of ['auto', 'true', 'false']) {
			const messages: string[] = [];
			const environment = { INPUT_FORMAT: format, INPUT_ANNOTATIONS: annotations, INPUT_SUMMARY: 'false' };
			await report(result, 'result.json', environment, reportOptions(environment), {
				log: (message) => messages.push(message),
				review: async () => assert.fail('review disabled'),
			});
			const enabled = annotations === 'true' || (annotations === 'auto' && (format === '' || format === 'github'));
			assert.deepEqual(messages, enabled ? [annotation(diagnostic)] : []);
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

test('JSON output and recovered failures preserve additive top-level result fields', async (t) => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-additive-result-'));
	t.after(() => rm(directory, { recursive: true }));
	const extended = {
		...result,
		documents: [{ kind: 'workflow', path: 'ci.yml', parse_status: 'complete', triggers: ['push'], jobs: [] }],
		statistics: { elapsed_ms: 12, rules: ['shellcheck'] },
		notices: ['compatible extension'],
		source_digest: null,
	};
	for (const failed of [false, true]) {
		const outputPath = join(directory, `outputs-${failed}`);
		const code = await withReporting({
			RUNNER_TEMP: directory,
			GITHUB_OUTPUT: outputPath,
			INPUT_FORMAT: 'json',
		}, async (environment) => {
			assert.ok(environment.ACTIONLINT_ACTION_RESULT);
			await writeFile(environment.ACTIONLINT_ACTION_RESULT, JSON.stringify(extended));
			if (failed) throw new Error('native process crashed');
			return 1;
		}, {
			log: () => {},
			review: async () => assert.fail('review disabled'),
		});
		assert.equal(code, failed ? 3 : 1);
		const outputs = await readOutputs(outputPath);
		assert.ok(outputs.output);
		assert.ok(outputs['result-file']);
		const expected = failed
			? { ...extended, completed: false, status: 'failure', exit_code: 3, error: 'native process crashed' }
			: extended;
		assert.deepEqual(JSON.parse(outputs.output), expected);
		assert.deepEqual(JSON.parse(await readFile(outputs['result-file'], 'utf8')), expected);
	}
});

test('native process failures preserve collected diagnostics and configuration', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-partial-test-'));
	try {
		for (const reject of [false, true]) {
			let resultPath = '';
			const outputPath = join(directory, `outputs-${reject}`);
			assert.equal(
				await withReporting(
					{ RUNNER_TEMP: directory, GITHUB_OUTPUT: outputPath, INPUT_SARIF: 'true' },
					async (environment) => {
						assert.ok(environment.ACTIONLINT_ACTION_RESULT);
						resultPath = environment.ACTIONLINT_ACTION_RESULT;
						await writeFile(resultPath, JSON.stringify(result));
						if (reject) throw new Error('native process crashed');
						return 3;
					},
				),
				3,
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

test('core output publication failure remains fatal and preserves analysis evidence', async (t) => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-output-failure-'));
	t.after(() => rm(directory, { recursive: true }));
	let resultPath = '';
	await assert.rejects(withReporting({ RUNNER_TEMP: directory, GITHUB_OUTPUT: directory }, async (environment) => {
		assert.ok(environment.ACTIONLINT_ACTION_RESULT);
		resultPath = environment.ACTIONLINT_ACTION_RESULT;
		await writeFile(resultPath, JSON.stringify(result));
		return 1;
	}));
	const persisted = parseResult(JSON.parse(await readFile(resultPath, 'utf8')));
	assert.equal(persisted.status, 'failure');
	assert.equal(persisted.completed, false);
	assert.equal(persisted.exit_code, 3);
	assert.deepEqual(persisted.diagnostics, result.diagnostics);
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
				'  inspectTools: async () => ({ shellcheck: false }),',
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
			}, { timeoutMS: 5_000 });
			assert.equal(child.exitCode, scenario.code, child.stderr);
			assert.ok(child.stdout.includes('::error::'));
			const outputs = await readFile(outputPath, 'utf8');
			const statuses = [...outputs.matchAll(/^result<<[^\n]+\n([^\n]+)\n/gm)].map((match) => match[1]);
			assert.deepEqual(statuses, [scenario.status]);
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
	const text = summary({ ...result, error: 'Missing <config>\nTry another path', hints: ['Check & retry'] });
	assert.ok(text.includes('&amp; &lt;value&gt;'));
	assert.ok(text.includes('<p>Quote this &amp; &lt;value&gt;<br>::warning::spoof</p>'));
	assert.ok(text.includes('<p>Missing &lt;config&gt;<br>Try another path</p>'));
	assert.ok(text.includes('<p>Check &amp; retry</p>'));
	assert.ok(text.includes('<pre>echo &quot;$foo&quot;</pre>'));
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
		assert.equal(messages.length, 2);
		assert.ok(messages[1]?.startsWith('::warning::PR review skipped: HTTP 403'));
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
		assert.equal(messages.filter((text) => text.startsWith('::warning file=')).length, 2);
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
		assert.equal(
			await withReporting({
				RUNNER_TEMP: directory,
				INPUT_ANNOTATIONS: 'invalid',
				INPUT_SUMMARY: 'false',
				GITHUB_STEP_SUMMARY: disabledSummary,
			}, async () => assert.fail('invalid inputs must stop before execution')),
			2,
		);
		await assert.rejects(readFile(disabledSummary), { code: 'ENOENT' });
		assert.equal(await withReporting({ RUNNER_TEMP: directory }, async () => 0), 3);
		assert.equal(
			await withReporting({ RUNNER_TEMP: directory }, async (environment) => {
				const path = environment.ACTIONLINT_ACTION_RESULT;
				assert.ok(path);
				await writeFile(path, '{"schema_version":');
				return 0;
			}),
			3,
		);
		const output = join(directory, 'outputs');
		assert.equal(
			await withReporting({ RUNNER_TEMP: directory }, async (environment) => {
				const path = environment.ACTIONLINT_ACTION_RESULT;
				assert.ok(path);
				await writeFile(path, JSON.stringify(result));
				return 3;
			}),
			3,
		);
		assert.equal(
			await withReporting({ RUNNER_TEMP: directory, GITHUB_OUTPUT: output }, async () => {
				throw new Error('download failed');
			}),
			3,
		);
		assert.ok((await readFile(output, 'utf8')).includes('result-file<<'));
	} finally {
		await rm(directory, { recursive: true });
	}
});
