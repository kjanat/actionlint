import assert from 'node:assert/strict';
import { test } from 'node:test';

import type { ActionResult, Diagnostic } from '#result';
import { diagnosticSnippet, reportStatus, summary } from '#summary';

const finding: Diagnostic = {
	rule: 'expression',
	message: 'Undefined variable "missing". See https://example.com/help for details.',
	path: 'ci.yml',
	start: { line: 6, column: 6 },
	end: { line: 6, column: 13 },
	snippet: 'echo missing',
};
const result: ActionResult = {
	schema_version: 1,
	completed: true,
	status: 'problems-found',
	exit_code: 1,
	file_count: 1,
	diagnostics: [finding],
	configurations: [],
	hints: [],
};

test('guidance links exclude prose punctuation and retain balanced URL delimiters', () => {
	for (
		const [input, expected] of [
			['See (https://example.com/help).', 'https://example.com/help'],
			['See https://example.com/help.', 'https://example.com/help'],
			['See [https://example.com/help],', 'https://example.com/help'],
			['See "https://example.com/help".', 'https://example.com/help'],
			['See (https://example.com/a_(b)).', 'https://example.com/a_(b)'],
			['See https://example.com/?q=a%29&n=1.', 'https://example.com/?q=a%29&amp;n=1'],
			['See https://[::1]/help.', 'https://[::1]/help'],
		]
	) {
		const text = summary({ ...result, diagnostics: [{ ...finding, message: input || '' }] });
		assert.ok(text.includes(`<${expected}>`), text);
	}
	assert.ok(
		summary({ ...result, diagnostics: [{ ...finding, message: 'See (https://example.com/help).' }] }).includes(
			'See \\(<https://example.com/help>\\)\\.',
		),
	);
});

test('findings are visible with their severity, actual rule, source range and help link', () => {
	const text = summary(result);
	assert.ok(text.includes('- **Result:** 1 error\n- **Workflows:** 1'));
	assert.ok(text.includes('### Findings\n\nUndefined variable "missing"'));
	assert.ok(text.includes('<https://example.com/help>'));
	assert.ok(text.includes('(error, expression)'));
	assert.ok(text.includes('```text\n6 | echo missing\n  |      ^~~~~~~\n```'));
	assert.doesNotMatch(text, /<details>|<pre>|&quot;|—|·|workflows checked/);
});

test('invocation titles can distinguish repeated runs without affecting machine output', () => {
	assert.equal(reportStatus(result, { GITHUB_ACTION: 'lint_2' }), 'actionlint (lint_2): 1 error');
	assert.equal(
		reportStatus(result, { GITHUB_ACTION: 'lint_2', 'INPUT_SUMMARY-TITLE': 'Production' }),
		'actionlint (Production): 1 error',
	);
	assert.ok(
		summary(result, { 'INPUT_SUMMARY-TITLE': '<img>\n# spoof' }).startsWith(
			'## actionlint \\(&lt;img&gt; \\# spoof\\)\n',
		),
	);
});

test('startup failures explain missing config once and retain unrelated errors', () => {
	const path = '/repo/.github/actionlint-missing.yaml';
	const failure: ActionResult = {
		...result,
		completed: false,
		status: 'failure',
		exit_code: 3,
		file_count: null,
		diagnostics: [],
		error: `could not read config file "${path}": read config "${path}": open ${path}: no such file or directory`,
	};
	const text = summary(failure, { GITHUB_WORKSPACE: '/repo' });
	assert.ok(text.includes('Could not start analysis'));
	assert.ok(text.includes('Configuration file not found: \\.github/actionlint\\-missing\\.yaml'));
	assert.ok(text.includes('ensure the file exists in the checkout'));
	assert.doesNotMatch(text, /\/repo|Exit 3|0 findings|No findings|\*\*Workflows/);
	assert.ok(summary({ ...failure, error: 'Permission denied' }).includes('Permission denied'));
	assert.ok(summary({ ...failure, diagnostics: [finding] }).includes('Partial results: 1 error.'));
	const windowsPath = String.raw`C:\repo\missing.yml`;
	assert.ok(
		summary({
			...failure,
			error: `could not read config file ${
				JSON.stringify(windowsPath)
			}: open ${windowsPath}: The system cannot find the file specified.`,
		}, { GITHUB_WORKSPACE: String.raw`C:\repo` }).includes('Configuration file not found: missing\\.yml'),
	);
});

test('caret ranges handle Unicode, tabs, EOF and multiline findings', () => {
	assert.equal(
		diagnosticSnippet({ ...finding, snippet: '界x', start: { line: 4, column: 2 }, end: { line: 4, column: 3 } }),
		'4 | 界x\n  |   ^',
	);
	assert.equal(
		diagnosticSnippet({ ...finding, snippet: 'e\u0301x', start: { line: 4, column: 3 }, end: { line: 4, column: 4 } }),
		'4 | e\u0301x\n  |  ^',
	);
	assert.equal(
		diagnosticSnippet({ ...finding, snippet: 'é\tx', start: { line: 4, column: 3 }, end: { line: 4, column: 4 } }),
		'4 | é    x\n  |      ^',
	);
	assert.equal(
		diagnosticSnippet({ ...finding, start: { line: 6, column: 13 }, end: { line: 6, column: 13 } }),
		'6 | echo missing\n  |             ^',
	);
	assert.equal(diagnosticSnippet({ ...finding, end: { line: 7, column: 2 } }), '6 | echo missing\n  |      ^~~~~~~');
});

test('long source lines show the finding in a bounded window', () => {
	const snippet = diagnosticSnippet({
		...finding,
		snippet: `${'a'.repeat(2500)}x`,
		start: { line: 6, column: 2501 },
		end: { line: 6, column: 2502 },
	});
	assert.equal(snippet, `6 | …${'a'.repeat(80)}x\n  | ${' '.repeat(81)}^`);
});

test('untrusted Markdown cannot escape snippets or create active links', () => {
	const text = summary({
		...result,
		diagnostics: [{ ...finding, message: '[click](javascript:alert(1)) <img src=x>', snippet: '```\n# injected' }],
	});
	assert.ok(text.includes('\\[click\\]\\(javascript:alert\\(1\\)\\) &lt;img src=x&gt;'));
	assert.ok(text.includes('````text\n6 | ```'));
	assert.ok(!text.includes('\n# injected'));
});

test('severity totals stay separate and unknown workflow counts are omitted', () => {
	const diagnostics = [finding, ...['warning', 'info', 'style'].map((severity) => ({ ...finding, severity }))];
	const text = summary({ ...result, diagnostics, file_count: null });
	assert.ok(text.includes('**Result:** 1 error, 1 warning, 1 notice, 1 style finding'));
	assert.ok(!text.includes('**Workflows:**'));
});
