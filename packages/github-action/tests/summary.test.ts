import assert from 'node:assert/strict';
import { test } from 'node:test';

import { execFileSync } from 'node:child_process';
import { mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { ActionResult, Diagnostic } from '#result';
import { sourceLinkVerifier } from '#source-links';
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

test('URLs from workflow values keep their visible destination', () => {
	const text = summary({
		...result,
		diagnostics: [{
			...finding,
			rule: 'runner-label',
			message: 'label "https://attacker.example/a?x=1&y=2" is unknown',
		}],
	});
	assert.ok(
		text.includes('<a href="https://attacker.example/a?x=1&amp;y=2">https://attacker.example/a?x=1&amp;y=2</a>'),
	);
	assert.ok(!text.includes('Documentation'));
});

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

test('source links canonicalize workspace paths and validate repository names', async () => {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-summary-links-'));
	try {
		const workspace = join(root, 'workspace');
		await mkdir(join(workspace, 'sub'), { recursive: true });
		await writeFile(join(workspace, 'sub', 'ci.yml'), 'source');
		await writeFile(join(root, 'ci.yml'), 'external');
		const filename = process.platform === 'win32' ? 'a #.yml' : 'a #?.yml';
		await writeFile(join(workspace, 'sub', filename), 'source');
		const git = (...args: string[]): string =>
			execFileSync('git', [
				'-C',
				workspace,
				'-c',
				'user.name=Test',
				'-c',
				'user.email=test@example.com',
				'-c',
				'commit.gpgsign=false',
				...args,
			], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
		git('init', '--template=');
		git('add', '.');
		git('commit', '-m', 'Fixture');
		git('remote', 'add', 'origin', 'https://github.example.com/owner/repo.git');
		const sha = git('rev-parse', 'HEAD');
		const environment = {
			GITHUB_REPOSITORY: 'owner/repo',
			GITHUB_SHA: sha,
			GITHUB_SERVER_URL: 'https://github.example.com',
			GITHUB_WORKSPACE: workspace,
			'INPUT_WORKING-DIRECTORY': 'sub',
		};
		const failures: string[] = [];
		const verifyCommitted = sourceLinkVerifier(environment, message => failures.push(message));
		const committedVerified = verifyCommitted(join('sub', filename));
		assert.equal(committedVerified, true, `Source verification failed: ${failures.join(', ')}`);
		const text = summary(
			{ ...result, diagnostics: [{ ...finding, path: filename, end: { line: 9, column: 1 } }] },
			environment,
		);
		const expectedLink = `href="https://github.example.com/owner/repo/blob/${sha}/sub/${
			encodeURIComponent(filename)
		}#L6-L8"`;
		assert.ok(text.includes(expectedLink), `Expected ${expectedLink} in summary:\n${text}`);
		assert.ok(text.includes(`<code>${filename}:6:6</code></a>`));
		for (
			const invalid of [{ GITHUB_SHA: '' }, { GITHUB_REPOSITORY: '../repo' }, { GITHUB_REPOSITORY: 'owner/..' }, {
				GITHUB_REPOSITORY: 'owner/.',
			}, {
				GITHUB_SERVER_URL: 'javascript:alert(1)',
			}]
		) {
			assert.ok(!summary(result, { ...environment, ...invalid }).includes('/blob/'));
		}
		assert.ok(
			!summary({ ...result, diagnostics: [{ ...finding, path: '../../ci.yml' }] }, environment).includes('/blob/'),
		);
		assert.ok(
			!summary({ ...result, diagnostics: [{ ...finding, path: 'missing.yml' }] }, environment).includes('/blob/'),
		);
		for (const repository of ['owner/.github', 'owner/.hidden']) {
			git('remote', 'set-url', 'origin', `git@github.example.com:${repository}.git`);
			assert.ok(summary(result, { ...environment, GITHUB_REPOSITORY: repository }).includes(`/${repository}/blob/`));
		}
		git('remote', 'set-url', 'origin', 'ssh://git@github.example.com/owner/repo.git');
		assert.ok(summary(result, environment).includes('/blob/'));
		const previousGitDir = process.env.GIT_DIR;
		const previousGlobalConfig = process.env.GIT_CONFIG_GLOBAL;
		const invalidConfig = join(root, 'invalid.gitconfig');
		await writeFile(invalidConfig, '[invalid');
		try {
			process.env.GIT_DIR = join(root, 'missing-git');
			process.env.GIT_CONFIG_GLOBAL = invalidConfig;
			assert.ok(summary(result, environment).includes('/blob/'));
		} finally {
			if (previousGitDir === undefined) delete process.env.GIT_DIR;
			else process.env.GIT_DIR = previousGitDir;
			if (previousGlobalConfig === undefined) delete process.env.GIT_CONFIG_GLOBAL;
			else process.env.GIT_CONFIG_GLOBAL = previousGlobalConfig;
		}
		assert.ok(!summary(result, { ...environment, GITHUB_SHA: '0'.repeat(40) }).includes('/blob/'));
		git('remote', 'set-url', 'origin', 'https://wrong.example.com/owner/repo.git');
		assert.ok(!summary(result, environment).includes('/blob/'));
		git('remote', 'set-url', 'origin', 'https://github.example.com/other/repo.git');
		assert.ok(!summary(result, environment).includes('/blob/'));
		git('remote', 'set-url', 'origin', 'https://github.example.com/owner/repo.git');
		await writeFile(join(workspace, 'sub', 'ci.yml'), 'modified');
		failures.length = 0;
		const verifyModified = sourceLinkVerifier(environment, message => failures.push(message));
		const modifiedVerified = verifyModified(join('sub', 'ci.yml'));
		assert.equal(modifiedVerified, false, 'Modified content must not link to the committed blob');
		assert.match(failures[0] || '', /committed blob: file content differs from the committed blob/);
		failures.length = 0;
		summary(result, environment, new Map(), message => failures.push(message));
		assert.match(failures[0] || '', /file content differs from the committed blob/);
		assert.ok(!summary(result, environment).includes('/blob/'));
		await writeFile(join(workspace, 'sub', 'ci.yml'), 'source');
		await writeFile(join(workspace, 'sub', 'untracked.yml'), 'source');
		failures.length = 0;
		const verifyUntracked = sourceLinkVerifier(environment, message => failures.push(message));
		const untrackedVerified = verifyUntracked(join('sub', 'untracked.yml'));
		assert.equal(untrackedVerified, false, 'Untracked files must not link to a nonexistent committed blob');
		assert.match(failures[0] || '', /committed blob: git cat-file: exit 128/);
		assert.ok(
			!summary({ ...result, diagnostics: [{ ...finding, path: 'untracked.yml' }] }, environment).includes('/blob/'),
		);
		assert.ok(
			!summary(result, { ...environment, GITHUB_WORKSPACE: root, 'INPUT_WORKING-DIRECTORY': '.' }).includes('/blob/'),
		);
		await symlink(
			join(workspace, 'sub'),
			join(workspace, 'internal'),
			process.platform === 'win32' ? 'junction' : 'dir',
		);
		await symlink(root, join(workspace, 'external'), process.platform === 'win32' ? 'junction' : 'dir');
		await symlink(workspace, join(root, 'linked-workspace'), process.platform === 'win32' ? 'junction' : 'dir');
		assert.ok(summary(result, { ...environment, 'INPUT_WORKING-DIRECTORY': 'internal' }).includes(`/sub/ci.yml#L6`));
		assert.ok(!summary(result, { ...environment, 'INPUT_WORKING-DIRECTORY': 'external' }).includes('/blob/'));
		assert.ok(
			summary(result, { ...environment, GITHUB_WORKSPACE: join(root, 'linked-workspace') }).includes('/sub/ci.yml#L6'),
		);
		execFileSync('git', ['-C', join(workspace, 'sub'), 'init', '--template='], { stdio: 'ignore' });
		assert.ok(!summary(result, environment).includes('/blob/'));
	} finally {
		await rm(root, { recursive: true, force: true });
	}
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
	assert.ok(text.includes('<pre lang="yaml">&lt;/pre&gt;'));
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
		assert.ok(text.includes(`<a href="${expected}">${expected}</a>`), text);
	}
	assert.ok(
		summary({ ...result, diagnostics: [{ ...finding, message: 'See (https://example.com/help).' }] }).includes(
			'See (<br><sub><a href="https://example.com/help">https://example.com/help</a></sub>).',
		),
	);
});

test('findings are visible with their severity, actual rule, source range and help link', () => {
	const text = summary(result);
	assert.ok(!text.includes('<strong>1 error'));
	assert.ok(text.includes('Undefined variable &quot;missing&quot;'));
	assert.ok(text.includes('<a href="https://example.com/help">https://example.com/help</a>'));
	assert.ok(text.includes('<p><code>ci.yml:6:6</code> | Rule: <code>expression</code></p>'));
	assert.ok(!text.includes('<sub><code>ci.yml'));
	assert.ok(text.includes('<p>🔴 <strong>Error:</strong> Undefined variable'));
	assert.doesNotMatch(text, /<h[23]>/);
	assert.ok(text.includes('<br><sub><a href="https://example.com/help">https://example.com/help</a></sub>'));
	assert.ok(!text.includes('See '));
	assert.ok(!text.includes('for details'));
	assert.ok(text.includes('<pre lang="yaml">echo missing&#10;     ^~~~~~~</pre>'));
	assert.doesNotMatch(text, /(?:> |&#10;)\d+ \| |&#10; *\|/);
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

test('summary keeps advice continuous and places documentation on its own line', () => {
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
			'<strong>Error:</strong> <code>github.event.head_commit.message</code> is unsafe. avoid inline expansion. instead use an environment variable.',
		),
	);
	assert.ok(text.includes('<br><sub><a href="https://example.com/help">https://example.com/help</a></sub>'));
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
	assert.ok(text.includes('<pre lang="yaml">```'));
	assert.ok(!text.includes('\n# injected'));
});

test('findings retain individual severities without count announcements', () => {
	const diagnostics = [finding, ...['warning', 'info', 'style'].map((severity) => ({ ...finding, severity }))];
	const text = summary({ ...result, diagnostics, file_count: null });
	for (const [severity, icon] of [['Error', '🔴'], ['Warning', '⚠️'], ['Notice', 'ℹ️'], ['Style', '💡']]) {
		assert.ok(text.includes(`<p>${icon} <strong>${severity}:</strong>`));
	}
	assert.doesNotMatch(text, /\d+ (?:errors?|warnings?|notices?|style findings?|workflows?)/);
	assert.equal(reportStatus({ ...result, diagnostics }), '');
	assert.ok(!text.includes('**Workflows:**'));
});
