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

test('quoted expression context references render as code without quoting prose', () => {
	const text = summary({
		...result,
		diagnostics: [{
			...finding,
			message:
				'Avoid "github.event.head_commit.message" and \'inputs.branch\'; keep "ordinary prose" and "github.<script>" literal.',
		}],
	});
	assert.ok(text.includes('<code>github.event.head_commit.message</code>'));
	assert.ok(text.includes('<code>inputs.branch</code>'));
	assert.ok(text.includes('&quot;ordinary prose&quot;'));
	assert.ok(text.includes('&quot;github.&lt;script&gt;&quot;'));
});

test('source locations link to the run commit, respecting working-directory and exclusive ends', () => {
	const environment = {
		GITHUB_REPOSITORY: 'owner/repo',
		GITHUB_SHA: 'a'.repeat(40),
		GITHUB_SERVER_URL: 'https://github.example.com',
		GITHUB_WORKSPACE: '/repo',
		'INPUT_WORKING-DIRECTORY': 'sub',
	};
	const text = summary(
		{ ...result, diagnostics: [{ ...finding, path: 'a #?.yml', end: { line: 9, column: 1 } }] },
		environment,
	);
	assert.ok(
		text.includes(`href="https://github.example.com/owner/repo/blob/${'a'.repeat(40)}/sub/a%20%23%3F.yml#L6-L8"`),
	);
	assert.ok(text.includes('<code>a #?.yml:6:6</code></a>'));
	for (
		const invalid of [{ GITHUB_SHA: '' }, { GITHUB_REPOSITORY: '../repo' }, {
			GITHUB_SERVER_URL: 'javascript:alert(1)',
		}]
	) {
		assert.ok(!summary(result, { ...environment, ...invalid }).includes('/blob/'));
	}
	assert.ok(
		!summary({ ...result, diagnostics: [{ ...finding, path: '../../outside.yml' }] }, environment).includes('/blob/'),
	);
});

test('HTML content and URL attributes cannot introduce markup', () => {
	const text = summary({
		...result,
		diagnostics: [{
			...finding,
			message: 'See https://example.com/"onmouseover="oops and </p><script>oops</script>',
			path: '</code><img src=x>',
			snippet: '</pre><script>oops</script>',
		}],
	});
	assert.ok(!text.includes('<script>'));
	assert.ok(!text.includes('<img'));
	assert.ok(text.includes('&quot;onmouseover=&quot;oops'));
	assert.ok(text.includes('<pre lang="yaml">6 | &lt;/pre&gt;'));
});

test('blank lines in untrusted fields cannot end HTML blocks and activate Markdown', () => {
	for (const newline of ['\n', '\r', '\r\n']) {
		const injected =
			`prefix${newline}${newline}![tracking](https://example.com/tracking.png)${newline}${newline}suffix`;
		const text = summary({
			...result,
			hints: [injected],
			diagnostics: [{ ...finding, path: injected, rule: injected, code: injected }],
		});
		assert.ok(!text.includes(`${newline}${newline}![tracking]`));
		assert.ok(text.includes('prefix&#10;&#10;![tracking]'));
		assert.ok(!text.includes('<img'));
	}
});

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
		assert.ok(text.includes(`<a href="${expected}">Documentation</a>`), text);
	}
	assert.ok(
		summary({ ...result, diagnostics: [{ ...finding, message: 'See (https://example.com/help).' }] }).includes(
			'See (<br><sub><a href="https://example.com/help">Documentation</a></sub>).',
		),
	);
});

test('findings are visible with their severity, actual rule, source range and help link', () => {
	const text = summary(result);
	assert.ok(!text.includes('<strong>1 error'));
	assert.ok(text.includes('Undefined variable &quot;missing&quot;'));
	assert.ok(text.includes('<a href="https://example.com/help">Documentation</a>'));
	assert.ok(text.includes('<sub><code>ci.yml:6:6</code> | Rule: <code>expression</code></sub>'));
	assert.ok(text.includes('<p><strong>Error:</strong> Undefined variable'));
	assert.doesNotMatch(text, /<h[23]>/);
	assert.ok(text.includes('<br><sub><a href="https://example.com/help">Documentation</a></sub>'));
	assert.ok(!text.includes('See '));
	assert.ok(!text.includes('for details'));
	assert.ok(text.includes('<pre lang="yaml">6 | echo missing&#10;  |      ^~~~~~~</pre>'));
	assert.doesNotMatch(text, /<details>|```text|### Summary|### Findings|—|·|workflows checked/);
});

test('invocation titles can distinguish repeated runs without affecting machine output', () => {
	assert.doesNotMatch(summary(result, { GITHUB_ACTION: 'lint_2' }), /lint_2|<h[23]>/);
	assert.equal(reportStatus(result, { GITHUB_ACTION: 'lint_2' }), '');
	assert.equal(
		reportStatus({ ...result, diagnostics: [] }, { GITHUB_ACTION: 'lint_2', 'INPUT_SUMMARY-TITLE': 'Production' }),
		'actionlint (Production): No findings',
	);
	assert.ok(
		summary(result, { 'INPUT_SUMMARY-TITLE': '<img>\n# spoof' }).startsWith(
			'<p><strong>&lt;img&gt; # spoof</strong></p>\n',
		),
	);
});

test('summary joins severity to diagnostic prose and breaks advice into lines', () => {
	const text = summary({
		...result,
		diagnostics: [{
			...finding,
			message:
				'"github.event.head_commit.message" is unsafe. avoid inline expansion. instead use an environment variable. See https://example.com/help for more details.',
		}],
	});
	assert.ok(
		text.includes(
			'<strong>Error:</strong> <code>github.event.head_commit.message</code> is unsafe.<br>avoid inline expansion.<br>instead use an environment variable.',
		),
	);
	assert.ok(text.includes('<br><sub><a href="https://example.com/help">Documentation</a></sub>'));
	assert.doesNotMatch(text, /<h[23]>|See https|for more details/);
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
	assert.ok(text.includes('Configuration file not found: .github/actionlint-missing.yaml'));
	assert.ok(text.includes('ensure the file exists in the checkout'));
	assert.doesNotMatch(text, /\/repo|Exit 3|0 findings|No findings|\*\*Workflows/);
	assert.ok(summary({ ...failure, error: 'Permission denied' }).includes('Permission denied'));
	assert.ok(
		summary({ ...failure, diagnostics: [finding] }).includes(
			'Analysis stopped before completion; further findings may exist.',
		),
	);
	const windowsPath = String.raw`C:\repo\missing.yml`;
	assert.ok(
		summary({
			...failure,
			error: `could not read config file ${
				JSON.stringify(windowsPath)
			}: open ${windowsPath}: The system cannot find the file specified.`,
		}, { GITHUB_WORKSPACE: String.raw`C:\repo` }).includes('Configuration file not found: missing.yml'),
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
	assert.ok(text.includes('[click](javascript:alert(1)) &lt;img src=x&gt;'));
	assert.ok(text.includes('<pre lang="yaml">6 | ```'));
	assert.ok(!text.includes('\n# injected'));
});

test('findings retain individual severities without count announcements', () => {
	const diagnostics = [finding, ...['warning', 'info', 'style'].map((severity) => ({ ...finding, severity }))];
	const text = summary({ ...result, diagnostics, file_count: null });
	for (const severity of ['Error', 'Warning', 'Notice', 'Style']) {
		assert.ok(text.includes(`<strong>${severity}:</strong>`));
	}
	assert.doesNotMatch(text, /\d+ (?:errors?|warnings?|notices?|style findings?|workflows?)/);
	assert.equal(reportStatus({ ...result, diagnostics }), '');
	assert.ok(!text.includes('**Workflows:**'));
});
