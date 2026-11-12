import assert from 'node:assert/strict';
import test, { mock } from 'node:test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import run, { bodyFor, groupsFromPatch, keyFor, readPullRequestFiles } from './comment-cop.mjs';

test('reads a fork diff through cached Git objects without checking out PR code', t => {
	const directory = mkdtempSync(join(tmpdir(), 'comment-cop-git-'));
	t.after(() => rmSync(directory, { recursive: true, force: true }));
	const origin = join(directory, 'origin');
	const cacheDirectory = join(directory, 'mise', 'cache', 'comment-cop', '42');
	mkdirSync(origin);
	/** @param {string[]} args */
	const git = args =>
		execFileSync('git', ['-C', origin, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
	git(['init', '-b', 'master']);
	git(['config', 'user.name', 'Test']);
	git(['config', 'user.email', 'test@example.invalid']);
	git(['config', 'commit.gpgsign', 'false']);
	git(['config', 'core.hooksPath', join(directory, 'no-hooks')]);
	writeFileSync(join(origin, 'shared.md'), 'Initial text.\n');
	git(['add', '.']);
	git(['commit', '-m', 'Initial revision']);
	const ancestor = git(['rev-parse', 'HEAD']);
	writeFileSync(join(origin, 'base-only.md'), 'Base branch update.\n');
	git(['add', '.']);
	git(['commit', '-m', 'Base branch update']);
	const baseSha = git(['rev-parse', 'HEAD']);
	git(['checkout', '-b', 'contribution', ancestor]);
	const filename = '[special] with spaces.md';
	const source = '```go\n// That said, example code is excluded.\n```\n\nThat said, scan this prose.\n';
	writeFileSync(join(origin, filename), source);
	git(['add', '.']);
	git(['commit', '-m', 'Contribution']);
	const headSha = git(['rev-parse', 'HEAD']);
	const options = { cacheDirectory, repositoryUrl: origin, baseSha, headSha };
	const first = readPullRequestFiles(options);
	assert.deepEqual(first.map(file => file.filename), [filename]);
	assert.equal(first[0].source, source);
	assert.deepEqual(groupsFromPatch(filename, first[0].patch, first[0].source).map(group => group.start), [5]);
	assert.deepEqual(readPullRequestFiles(options), first);
	const objects = join(cacheDirectory, 'repository.git');
	assert.equal(
		execFileSync('git', ['--git-dir', objects, 'rev-parse', '--is-bare-repository'], { encoding: 'utf8' }).trim(),
		'true',
	);
	assert.doesNotMatch(readFileSync(join(objects, 'config'), 'utf8'), /extraheader|token|\[remote/);
	git(['rm', filename]);
	writeFileSync(join(origin, 'replacement.md'), 'Moreover, scan the replacement.\n');
	git(['add', '.']);
	git(['commit', '-m', 'Replace contribution']);
	const updated = readPullRequestFiles({ ...options, headSha: git(['rev-parse', 'HEAD']) });
	assert.deepEqual(updated.map(file => file.filename), ['replacement.md']);
});

test('rejects invalid Git revisions before fetching', () => {
	assert.throws(() =>
		readPullRequestFiles({
			cacheDirectory: 'unused',
			repositoryUrl: 'unused',
			baseSha: '--upload-pack=bad',
			headSha: 'a'.repeat(40),
		}), /full Git commit SHA/);
});

test('reads every source and patch in a large diff with Unicode and binary files', t => {
	const directory = mkdtempSync(join(tmpdir(), 'comment-cop-batch-'));
	t.after(() => rmSync(directory, { recursive: true, force: true }));
	const origin = join(directory, 'origin');
	mkdirSync(origin);
	const git = (/** @type {string[]} */ args) =>
		execFileSync('git', ['-C', origin, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
	git(['init', '--quiet', '--initial-branch=master', '--template=']);
	git(['config', 'user.name', 'Test']);
	git(['config', 'user.email', 'test@example.invalid']);
	git(['config', 'commit.gpgsign', 'false']);
	git(['config', 'core.hooksPath', join(directory, 'no-hooks')]);
	git(['commit', '--quiet', '--allow-empty', '-m', 'Base']);
	const baseSha = git(['rev-parse', 'HEAD']);
	const expected = new Map();
	for (let index = 0; index < 1_000; index++) {
		const name = `file-${String(index).padStart(4, '0')}.md`;
		const source = `That said, café 😀 ${index}.\n\ndiff --git this is prose\n`;
		expected.set(name, source);
	}
	expected.set('[binary] with spaces.md', 'Binary\0content\n');
	for (const [name, source] of expected) writeFileSync(join(origin, name), source);
	writeFileSync(join(origin, 'unsupported.txt'), 'Not a scanned file.');
	git(['add', '.']);
	git(['commit', '--quiet', '-m', 'Contribution']);
	const files = readPullRequestFiles({
		cacheDirectory: join(directory, 'cache'),
		repositoryUrl: origin,
		baseSha,
		headSha: git(['rev-parse', 'HEAD']),
	});
	assert.equal(files.length, expected.size);
	for (const { filename, source, patch } of files) {
		assert.equal(source, expected.get(filename));
		if (filename.startsWith('file-')) {
			assert.deepEqual(groupsFromPatch(filename, patch, source).map(group => group.start), [1]);
		}
	}
});

/** @typedef {Parameters<Parameters<typeof run>[0]['github']['rest']['pulls']['createReview']>[0]} ReviewParams */

for (const scenario of ['unsupported patch', 'combined patches', 'combined blobs', 'single patch and blob']) {
	test(`reads supported files beyond the subprocess buffer limit: ${scenario}`, t => {
		const directory = mkdtempSync(join(tmpdir(), 'comment-cop-volume-'));
		t.after(() => rmSync(directory, { recursive: true, force: true }));
		const origin = join(directory, 'origin');
		mkdirSync(origin);
		const git = (/** @type {string[]} */ args) =>
			execFileSync('git', ['-C', origin, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
		git(['init', '--quiet', '--initial-branch=master', '--template=']);
		git(['config', 'user.name', 'Test']);
		git(['config', 'user.email', 'test@example.invalid']);
		git(['config', 'commit.gpgsign', 'false']);
		git(['config', 'core.hooksPath', join(directory, 'no-hooks')]);
		const line = 'That said, café 😀.\n';
		const expected = scenario === 'unsupported patch'
			? new Map([['small.md', line]])
			: scenario === 'single patch and blob'
			? new Map([['large.md', line.repeat(4_000)]])
			: new Map(Array.from({ length: 4 }, (_, index) => [`file-${index}.md`, line.repeat(400)]));
		if (scenario === 'combined blobs') {
			for (const [name, source] of expected) writeFileSync(join(origin, name), source.replace('That said', 'Old text'));
			git(['add', '.']);
		}
		git(['commit', '--quiet', '--allow-empty', '-m', 'Base']);
		const baseSha = git(['rev-parse', 'HEAD']);
		for (const [name, source] of expected) writeFileSync(join(origin, name), source);
		if (scenario === 'unsupported patch') writeFileSync(join(origin, 'unsupported.txt'), line.repeat(4_000));
		git(['add', '.']);
		git(['commit', '--quiet', '-m', 'Contribution']);
		const files = readPullRequestFiles({
			cacheDirectory: join(directory, 'cache'),
			repositoryUrl: origin,
			baseSha,
			headSha: git(['rev-parse', 'HEAD']),
			maxBuffer: 16 * 1_024,
		});
		assert.deepEqual(files.map(({ filename, source }) => [filename, source]), [...expected]);
		for (const { patch, source } of files) {
			assert.deepEqual(
				patch.split('\n').filter(row => row.startsWith('+') && !row.startsWith('+++'))
					.map(row => row.slice(1)),
				scenario === 'combined blobs' ? [line.trimEnd()] : source.trimEnd().split('\n'),
			);
		}
		assert.deepEqual(readdirSync(join(directory, 'cache')), ['repository.git']);
	});
}

test('flags a long implementation comment', () => {
	const groups = groupsFromPatch(
		'rule.go',
		`\
@@ -0,0 +1,4 @@
+\t// Parse the value here.
+\t// Keep the original around.
+\t// Return both values.
+\tparse(value)
`,
	);

	assert.deepEqual(groups.map(group => group.reasons), [['3 lines']]);
});

test('Go directives separate prose in complete and partial patches', () => {
	for (
		const directive of [
			'//go:generate go run generator.go',
			'//go:generate\tgo run generator.go',
			'//go:build linux && amd64',
			'//go:embed assets/*',
			'\t//go:embed assets/*',
			'//go:noinline',
			'//line generated.go:12',
			'//line generated.go:12:3',
		]
	) {
		const lines = [directive, '// Name generated files explicitly; directory walks skip them.', directive];
		const source = ['package fixture', '', ...lines].join('\n');
		for (
			const patch of [
				`@@ -0,0 +1,${source.split('\n').length} @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`,
				`@@ -2,1 +2,4 @@\n \n${lines.map(line => `+${line}`).join('\n')}`,
			]
		) {
			for (const contents of [undefined, source]) {
				assert.deepEqual(groupsFromPatch('generate.go', patch, contents), [], directive);
			}
		}
	}
});

test('directive-adjacent prose retains style and length findings', () => {
	const source = [
		'//go:generate echo "robust rather than elegant"',
		'// Use the cache rather than parsing twice.',
		'//go:generate go run generator.go',
		'// Parse the value.',
		'// Keep its position.',
		'// Return its diagnostics.',
		'//go:generate gofmt generated.go',
	].join('\n');
	const patches = [
		`@@ -0,0 +1,7 @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`,
		`@@ -1,1 +1,7 @@\n ${source.split('\n')[0]}\n${source.split('\n').slice(1).map(line => `+${line}`).join('\n')}`,
	];
	for (const patch of patches) {
		for (const contents of [undefined, source]) {
			assert.deepEqual(
				groupsFromPatch('generate.go', patch, contents).map(group => ({
					start: group.start,
					end: group.end,
					reasons: group.reasons,
				})),
				[
					{ start: 2, end: 2, reasons: ['"X rather than Y"'] },
					{ start: 4, end: 6, reasons: ['3 lines'] },
				],
			);
		}
	}
});

test('ordinary directive-like comments remain prose', () => {
	for (
		const line of [
			'// go:generate source files.',
			'//go: generate source files.',
			'//go:generate',
			'\t//go:generate go run generator.go',
			'//line generated.go:unknown',
			'\t//line generated.go:12',
		]
	) {
		const patch = `@@ -0,0 +1,3 @@\n${
			[line, '// Read the input.', '// Preserve its range.'].map(line => `+${line}`).join('\n')
		}`;
		assert.deepEqual(groupsFromPatch('generate.go', patch).map(group => group.reasons), [['3 lines']], line);
	}
	const patch = '@@ -0,0 +1,3 @@\n+//go:embed assets/*\n+// Read the input.\n+// Preserve its range.';
	assert.deepEqual(groupsFromPatch('generate.js', patch).map(group => group.reasons), [['3 lines']]);
});

test('Go directives preserve adjacent declaration documentation', () => {
	const source = [
		'// Parse reads the input.',
		'// It preserves source positions.',
		'// It returns every diagnostic.',
		'//go:noinline',
		'func Parse() {}',
	].join('\n');
	const patch = `@@ -1,2 +1,5 @@\n${
		source.split('\n').slice(0, 3).map(line => `+${line}`).join('\n')
	}\n //go:noinline\n func Parse() {}`;
	assert.deepEqual(groupsFromPatch('parse.go', patch, source), []);
});

test('scans prose around commented YAML examples without counting example lines', () => {
	const source = [
		'# Tools are discovered on PATH.',
		'# Python scripts are linted without execution.',
		'# tools:',
		'#   ruff: {select: [F, B, S]}',
		'#   shellcheck:',
		'#     # Inline directives remain active.',
		'#     config: {shell: bash}',
		'',
		'# local-actions:',
		'#   ./generated-action: ./src/action',
		'# files:',
		'#   includes:',
		'#     - "**/*.yaml"',
		'#     - "rather than.yml"',
		'# ignores:',
		'#   - rule: shellcheck',
		'#     reason: Use the robust compatibility wrapper rather than the default.',
		'',
		'# Use defaults rather than loading another file.',
	].join('\n');
	const patch = `@@ -0,0 +1,${source.split('\n').length} @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`;
	const groups = groupsFromPatch('config.yaml', patch, source);
	assert.deepEqual(groups.map(group => ({ start: group.start, reasons: group.reasons })), [
		{ start: 19, reasons: ['"X rather than Y"'] },
	]);
});

test('still measures consecutive YAML prose comments by length', () => {
	const patch = '@@ -0,0 +1,4 @@\n+# Read the settings.\n+# Resolve the paths.\n+# Return the result.\n+enabled: true';
	assert.deepEqual(groupsFromPatch('config.yml', patch).map(group => group.reasons), [['3 lines']]);
});

test('still scans prose list items in YAML comments', () => {
	const patch = '@@ -0,0 +1,1 @@\n+# - Use the cache rather than parsing twice.';
	assert.deepEqual(groupsFromPatch('config.yml', patch).map(group => group.reasons), [['"X rather than Y"']]);
});

test('scans labeled YAML prose independently of nearby examples', () => {
	const source = [
		'# Note: use the robust wrapper rather than the default.',
		'# tools:',
		'#   ruff:',
		'#     select: [F, B, S]',
		'# Warning: use the robust wrapper rather than the default.',
		'# note: robust',
	].join('\n');
	const patch = `@@ -0,0 +1,6 @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`;
	for (const contents of [undefined, source]) {
		const groups = groupsFromPatch('config.yaml', patch, contents);
		assert.deepEqual(groups.map(group => ({ start: group.start, reasons: group.reasons })), [
			{ start: 1, reasons: ['"X rather than Y"', 'inflated diction'] },
			{ start: 5, reasons: ['"X rather than Y"', 'inflated diction'] },
		]);
	}
});

test('recognizes unchanged YAML parents around partial diff hunks', () => {
	const source = '# tools:\n#   ruff:\n#     select: [F, B, S]\n#     ignore: [F401]\n#     enabled: true';
	const patch = '@@ -2,2 +2,4 @@\n #   ruff:\n+#     select: [F, B, S]\n+#     ignore: [F401]\n+#     enabled: true';
	assert.deepEqual(groupsFromPatch('config.yml', patch, source), []);
});

test('does not flag the generated configuration examples by length', () => {
	const source = readFileSync(
		new URL('../../../internal/input/configuration/config_default.yaml', import.meta.url),
		'utf8',
	);
	const lines = source.split('\n');
	const patch = `@@ -0,0 +1,${lines.length} @@\n${lines.map(line => `+${line}`).join('\n')}`;
	const groups = groupsFromPatch('config_default.yaml', patch, source);
	const examplesStart = lines.findIndex(line => line.startsWith('# Plugins check scripts')) + 1;
	assert.ok(examplesStart > 0);
	assert.deepEqual(groups.filter(group => group.start >= examplesStart), []);
});

test('does not measure Go doc comments by length', () => {
	const groups = groupsFromPatch(
		'rule.go',
		`\
@@ -0,0 +1,4 @@
+// Parser reads workflows.
+// It reports invalid syntax.
+// It returns every diagnostic.
+type Parser struct{}
`,
	);

	assert.deepEqual(groups, []);
});

test('does not measure Go field documentation by length', () => {
	const patch = [
		'@@ -1,2 +1,5 @@',
		' type Metadata struct {',
		'+\t// Defaults holds input values.',
		'+\t// Each value includes its position.',
		'+\t// Values remain in source order.',
		'+\tDefaults []*Value',
		' }',
	].join('\n');

	assert.deepEqual(groupsFromPatch('metadata.go', patch), []);
});

test('recognizes a Go doc comment when its declaration follows unchanged lines', () => {
	const source = [
		'// Check scans expressions.',
		'// It preserves source positions.',
		'// It reports unavailable contexts.',
		'// Invalid expressions end the scan.',
		'func Check() {}',
	].join('\n');
	const patch = [
		'@@ -1,3 +1,5 @@',
		'-// Check scans source.',
		'+// Check scans expressions.',
		'+// It preserves source positions.',
		'+// It reports unavailable contexts.',
		' // Invalid expressions end the scan.',
		' func Check() {}',
	].join('\n');

	assert.deepEqual(groupsFromPatch('rule.go', patch, source), []);
});

test('uses the unchanged first line to recognize partial field documentation', () => {
	const source = [
		'type Metadata struct {',
		'\t// Defaults holds input values.',
		'\t// Each value includes its position.',
		'\t// Values remain in source order.',
		'\t// The checker reads these values.',
		'\tDefaults []*Value',
		'}',
	].join('\n');
	const patch = [
		'@@ -2,2 +2,5 @@',
		' \t// Defaults holds input values.',
		'+\t// Each value includes its position.',
		'+\t// Values remain in source order.',
		'+\t// The checker reads these values.',
		' \tDefaults []*Value',
	].join('\n');

	assert.deepEqual(groupsFromPatch('metadata.go', patch, source), []);
});

test('flags style tells at any length', () => {
	const groups = groupsFromPatch(
		'rule.go',
		`\
@@ -0,0 +1,2 @@
+\t// Use the cache rather than parsing twice.
+\treturn cache
`,
	);

	assert.deepEqual(groups.map(group => group.reasons), [['"X rather than Y"']]);
});

test('scans Markdown prose but skips fenced code', () => {
	const groups = groupsFromPatch(
		'docs/checks.md',
		`\
@@ -0,0 +1,7 @@
+That said, this paragraph is prose.
+
+\`\`\`go
+// This comment uses robust machinery.
+\`\`\`
+
+- Moreover, this item is separate.
`,
	);

	assert.deepEqual(
		groups.map(group => group.reasons),
		[['filler phrase'], ['connective glue']],
	);
});

test('restores Markdown fence state before each diff hunk', () => {
	const source = [
		'# Example',
		'',
		'````markdown',
		'This contains a shorter ``` marker.',
		'Still inside the fence.',
		'That said, this is code-fence content.',
		'````',
	].join('\n');
	const patch = [
		'@@ -5,0 +6,1 @@',
		'+That said, this is code-fence content.',
	].join('\n');

	assert.deepEqual(groupsFromPatch('docs/checks.md', patch, source), []);
});

test('ignores unsupported file types', () => {
	const groups = groupsFromPatch(
		'fixture.txt',
		`\
@@ -0,0 +1,3 @@
+// one
+// two
+// three
`,
	);

	assert.deepEqual(groups, []);
});

test('uses opaque location-specific marker keys', () => {
	const groups = groupsFromPatch(
		'docs/design notes.md',
		`\
@@ -0,0 +1,1 @@
+That said, repeated prose.
@@ -9,0 +10,1 @@
+That said, repeated prose.
`,
	);

	const keys = groups.map(keyFor);
	assert.equal(keys.length, 2);
	assert.match(keys[0], /^[a-f0-9]{16}$/);
	assert.match(keys[1], /^[a-f0-9]{16}$/);
	assert.notEqual(keys[0], keys[1]);
});

test('links review comments to the shared writing instructions', () => {
	const group = { path: 'rule.go', start: 1, end: 3, text: '// Explanation', reasons: ['3 lines'] };
	assert.ok(readFileSync(new URL('../../../.agents/writing.md', import.meta.url), 'utf8').length > 0);
	for (const reasons of [['3 lines'], ['counterfactual justification']]) {
		const body = bodyFor({ ...group, reasons });
		assert.ok(body.includes(`Flagged for: ${reasons.join(', ')}.`));
		assert.ok(
			body.endsWith('[Writing instructions](https://github.com/kjanat/actionlint/blob/master/.agents/writing.md)'),
		);
		assert.doesNotMatch(body, /<sub>|```/);
	}
});

test('reports several reasons with one writing-instructions link', () => {
	const group = {
		path: 'rule.go',
		start: 1,
		end: 3,
		text: '// Explanation',
		reasons: ['3 lines', '"X instead of Y"', '"X rather than Y"'],
	};
	const body = bodyFor(group);

	assert.ok(body.includes('Flagged for: 3 lines, "X instead of Y", "X rather than Y".'));
	assert.equal(body.match(/\[Writing instructions\]/g)?.length, 1);
	assert.equal(body.split('\n\n').length, 2);
});

const reviewFiles = [
	{
		filename: 'docs/first.md',
		patch:
			'@@ -0,0 +1,5 @@\n+That said, the cache stores values.\n+Read the cached entry.\n+\n+\n+Moreover, refresh the entry.',
	},
	{
		filename: 'docs/second.md',
		patch: '@@ -0,0 +1,1 @@\n+Use the cache rather than fetching again.',
	},
];
const firstGroup = {
	path: 'docs/first.md',
	start: 1,
	end: 2,
	text: 'That said, the cache stores values.\nRead the cached entry.',
	reasons: ['filler phrase'],
};

/** @param {string[]} seenBodies @param {Error | undefined} submitError */
function reviewHarness(
	seenBodies = [],
	submitError = undefined,
	files = reviewFiles,
	threadPath = 'docs/first.md',
	source = 'Plain Markdown with no code fences.',
) {
	const createReview = mock.fn(async (/** @type {ReviewParams} */ params) => {
		if (submitError) throw submitError;
		return params;
	});
	const warning = mock.fn();
	const info = mock.fn();
	const graphql = mock.fn(async () => ({
		repository: {
			pullRequest: {
				reviewThreads: {
					pageInfo: { hasNextPage: false, endCursor: null },
					nodes: seenBodies.map((body, i) => ({
						id: `thread-${i}`,
						isResolved: false,
						path: threadPath,
						comments: { nodes: [{ body, viewerDidAuthor: true }] },
					})),
				},
			},
		},
	}));
	const args = {
		files: files.map(file => ({ filename: file.filename, patch: file.patch, source })),
		github: {
			rest: { pulls: { listFiles: mock.fn(), createReview } },
			paginate: mock.fn(async () => {
				throw new Error('File API must not be used');
			}),
			request: mock.fn(async () => {
				throw new Error('Contents API must not be used');
			}),
			graphql,
		},
		context: {
			repo: { owner: 'owner', repo: 'repo' },
			payload: { pull_request: { number: 42, head: { sha: 'a'.repeat(40) } } },
		},
		core: { warning, info },
	};
	return {
		args: /** @type {Parameters<typeof run>[0]} */ (/** @type {unknown} */ (args)),
		createReview,
		paginate: args.github.paginate,
		request: args.github.request,
		warning,
		info,
		graphql,
	};
}

test('submits comments across files and line ranges in one review', async () => {
	const h = reviewHarness();
	await run(h.args);

	assert.equal(h.createReview.mock.callCount(), 1);
	const params = h.createReview.mock.calls[0].arguments[0];
	assert.deepEqual(params, {
		owner: 'owner',
		repo: 'repo',
		pull_number: 42,
		commit_id: 'a'.repeat(40),
		event: 'COMMENT',
		body: 'Please review the flagged wording in the inline comments.',
		comments: [
			{ path: 'docs/first.md', line: 2, side: 'RIGHT', body: bodyFor(firstGroup), start_line: 1, start_side: 'RIGHT' },
			{
				path: 'docs/first.md',
				line: 5,
				side: 'RIGHT',
				body: bodyFor({
					...firstGroup,
					start: 5,
					end: 5,
					text: 'Moreover, refresh the entry.',
					reasons: ['connective glue'],
				}),
			},
			{
				path: 'docs/second.md',
				line: 1,
				side: 'RIGHT',
				body: bodyFor({
					path: 'docs/second.md',
					start: 1,
					end: 1,
					text: 'Use the cache rather than fetching again.',
					reasons: ['"X rather than Y"'],
				}),
			},
		],
	});
	assert.equal(h.warning.mock.callCount(), 0);
	assert.equal(h.paginate.mock.callCount(), 0);
	assert.equal(h.request.mock.callCount(), 0);
	assert.match(h.info.mock.calls[0].arguments[0], /3 posted/);

	assert.ok(params.comments);
	const rerun = reviewHarness(params.comments.map(comment => comment.body));
	await run(rerun.args);
	assert.equal(rerun.createReview.mock.callCount(), 0);
	assert.equal(rerun.graphql.mock.callCount(), 1);
});

test('excludes existing findings from the next review', async () => {
	const h = reviewHarness([bodyFor(firstGroup)]);
	await run(h.args);

	assert.equal(h.createReview.mock.callCount(), 1);
	const params = h.createReview.mock.calls[0].arguments[0];
	assert.ok(params);
	assert.ok(params.comments);
	assert.deepEqual(params.comments.map(({ path, line }) => ({ path, line })), [
		{ path: 'docs/first.md', line: 5 },
		{ path: 'docs/second.md', line: 1 },
	]);
});

test('does not submit an empty review when the diff has no findings', async () => {
	const h = reviewHarness([], undefined, []);
	await run(h.args);

	assert.equal(h.createReview.mock.callCount(), 0);
	assert.match(h.info.mock.calls[0].arguments[0], /0 posted/);
});

test('resolves an existing finding caused by commented YAML data', async () => {
	const path = 'config.yaml';
	const text = '# tools:\n#   ruff:\n#     select: [F, B, S]';
	const oldGroup = { path, start: 1, end: 3, text, reasons: ['3 lines'] };
	const files = [{
		filename: path,
		patch: `@@ -0,0 +1,3 @@\n${text.split('\n').map(line => `+${line}`).join('\n')}`,
	}];
	const h = reviewHarness([bodyFor(oldGroup)], undefined, files, path, text);
	await run(h.args);

	assert.equal(h.createReview.mock.callCount(), 0);
	assert.equal(h.graphql.mock.callCount(), 2);
	assert.deepEqual(h.graphql.mock.calls[1].arguments.slice(1), [{ id: 'thread-0' }]);
	assert.match(h.info.mock.calls[0].arguments[0], /1 stale threads resolved/);
});

test('reports a failed batch without submitting individual reviews', async () => {
	const h = reviewHarness([], new Error('Review rejected'));
	await run(h.args);

	assert.equal(h.createReview.mock.callCount(), 1);
	assert.deepEqual(h.warning.mock.calls[0].arguments, ['Could not submit Comment Cop review: Review rejected']);
	assert.match(h.info.mock.calls[0].arguments[0], /0 posted/);
});

test('recognizes consecutive scalar YAML examples while retaining prose labels', () => {
	const source = [
		'# Note: use the robust wrapper rather than the default.',
		'# image: ubuntu-latest',
		'# shell: bash',
		'# working-directory: src',
		'# Warning: use the robust wrapper rather than the default.',
	].join('\n');
	const patch = `@@ -0,0 +1,5 @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`;
	for (const contents of [undefined, source]) {
		assert.deepEqual(
			groupsFromPatch('config.yml', patch, contents).map(group => ({ start: group.start, reasons: group.reasons })),
			[
				{ start: 1, reasons: ['"X rather than Y"', 'inflated diction'] },
				{ start: 5, reasons: ['"X rather than Y"', 'inflated diction'] },
			],
		);
	}
});

test('recognizes scalar mapping keys in sequence items and partial hunks', () => {
	for (const prefix of ['- ', '  - ', '-   ']) {
		for (const key of ['name', '"name"', "'name'"]) {
			const source = [
				`# ${prefix}${key}: build rather than deploy`,
				`# ${' '.repeat(prefix.length)}run: echo rather than deploy`,
				`# ${' '.repeat(prefix.length)}shell: bash`,
			].join('\n');
			const patch = `@@ -0,0 +1,3 @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`;
			for (const contents of [undefined, source]) {
				assert.deepEqual(groupsFromPatch('workflow.yml', patch, contents), []);
			}
			assert.deepEqual(groupsFromPatch('workflow.yml', `@@ -1 +1 @@\n+${source.split('\n')[0]}`, source), []);
		}
	}
});

test('recognizes a standalone sequence mapping while retaining labeled prose', () => {
	assert.deepEqual(groupsFromPatch('config.yml', '@@ -0,0 +1 @@\n+# - name: build rather than deploy'), []);
	assert.deepEqual(
		groupsFromPatch('config.yml', '@@ -0,0 +1 @@\n+# - Note: use this rather than that').map(group => group.reasons),
		[['"X rather than Y"']],
	);
});

test('recognizes commented block scalars and restores prose at their boundary', () => {
	for (const indicator of ['|', '|-', '|+', '>', '>-', '>+', '|2', '|2-', '|-2', '>2+', '>+2', '| # script']) {
		for (const [header, indent] of [['run:', '  '], ['- run:', '    '], ['-', '  '], ['  run:', '    ']]) {
			const source = [
				`# ${header} ${indicator}`,
				`# ${indent}echo use this rather than that`,
				'#',
				`# ${indent}Note: use this rather than that`,
				'# Note: use this rather than that',
			].join('\n');
			const patch = `@@ -0,0 +1,5 @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`;
			for (const contents of [undefined, source]) {
				assert.deepEqual(
					groupsFromPatch('workflow.yml', patch, contents).map(group => ({
						start: group.start,
						reasons: group.reasons,
					})),
					[{ start: 5, reasons: ['"X rather than Y"'] }],
				);
			}
			assert.deepEqual(groupsFromPatch('workflow.yml', `@@ -2 +2 @@\n+${source.split('\n')[1]}`, source), []);
		}
	}
});
