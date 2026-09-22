import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

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
	assert.deepEqual(reportOptions({}), { annotations: false, summary: false, json: false, sarif: false, review: false });
	assert.deepEqual(reportOptions({ 'INPUT_REPORT-FORMATS': 'json, sarif\njson', INPUT_SUMMARY: 'true' }), {
		annotations: false,
		summary: true,
		json: true,
		sarif: true,
		review: false,
	});
	assert.throws(() => reportOptions({ INPUT_REVIEW: 'maybe' }));
	assert.throws(() => reportOptions({ 'INPUT_REPORT-FORMATS': 'xml' }));
});

test('annotation commands and Markdown summaries escape diagnostic content', () => {
	assert.equal(
		annotation(diagnostic),
		'::warning file=a%2Cb.yml,line=2,endLine=2,col=3,endColumn=7,title=SC2086::Quote this & <value>%0A::warning::spoof',
	);
	const text = summary(result);
	assert.ok(text.includes('&amp; &lt;value&gt;'));
	assert.ok(!text.includes('<value>'));
	assert.ok(text.includes('1 finding in 2 workflow files'));
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
			{ annotations: true, summary: true, json: true, sarif: true, review: true },
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
		assert.ok((await readFile(output, 'utf8')).includes('report-json<<'));
		await report(result, path, { INPUT_FORMAT: 'json' }, {
			annotations: true,
			summary: false,
			json: false,
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

test('missing or corrupt native results cannot become a clean analysis', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-incomplete-test-'));
	try {
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
		assert.ok((await readFile(output, 'utf8')).includes('analysis-result<<'));
	} finally {
		await rm(directory, { recursive: true });
	}
});
