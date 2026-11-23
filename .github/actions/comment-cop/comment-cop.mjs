#!/usr/bin/env node
// @ts-check

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { closeSync, fstatSync, mkdirSync, mkdtempSync, openSync, readFileSync, readSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { StringDecoder } from 'node:string_decoder';

import { yamlExampleLines } from './yaml-examples.mjs';

/** @typedef {'go' | 'js' | 'hash' | 'md'} Lang */
/** @typedef {{path: string, start: number, end: number, text: string, reasons: string[]}} Group */
/** @typedef {{start: number, end: number, lang: Lang, topLevel: boolean, doc: boolean, lines: string[]}} PendingGroup */
/** @typedef {{id: string, isResolved: boolean, path: string, comments: {nodes: Array<{body: string, viewerDidAuthor: boolean}>}}} ReviewThread */
/** @typedef {{repository: {pullRequest: {reviewThreads: {pageInfo: {hasNextPage: boolean, endCursor: string | null}, nodes: ReviewThread[]}}}}} ReviewThreadsResponse */
/** @typedef {{filename: string, patch: string, source: string}} ChangedFile */
/** @typedef {import('@actions/github-script').AsyncFunctionArguments} ScriptArguments */
/** @typedef {Parameters<ScriptArguments['github']['rest']['pulls']['createReview']>[0]} ReviewParams */
/**
 * @typedef {object} RunArguments
 * @property {{
 *   graphql: {
 *     (query: string, parameters: {owner: string, repo: string, pr: number, after: string | null}): Promise<ReviewThreadsResponse>,
 *     (query: string, parameters: {id: string}): Promise<unknown>,
 *   },
 *   rest: {pulls: {createReview: (params: ReviewParams) => Promise<unknown>}},
 * }} github
 * @property {{repo: {owner: string, repo: string}, payload: {pull_request?: {number: number, head: {sha: string}}}}} context
 * @property {Pick<ScriptArguments['core'], 'info' | 'warning'>} core
 * @property {ChangedFile[]} files
 */
/** @typedef {{character: '`' | '~', length: number} | null} Fence */

/** @type {Array<[RegExp, Lang]>} */
const LANGS = [
	[/\.go$/, 'go'],
	[/\.(?:mjs|cjs|js|ts|jsonc)$/, 'js'],
	[/\.(?:ya?ml|py|sh|bash|toml)$/, 'hash'],
	[/(?:^|\/)(?:Makefile|Dockerfile)(?:\.[\w.-]+)?$/, 'hash'],
	[/\.md$/, 'md'],
];

/** @type {Array<[string, RegExp]>} */
const TELLS = [
	[
		'em dash',
		/[—–]/,
	],
	['"X, not Y"', /,[\s]+not\s+\S/],
	['"X rather than Y"', /\brather than\b/i],
	['"X instead of Y"', /\b(?:instead of|as opposed to)\b/i],
	['"not just X but Y"', /\bnot (?:just|merely|only|because)\b[^.]{0,80}?\bbut\b/i],
	[
		'emphatic cleft',
		/\b(?:which|that) is (?:what|why|how)\b|\bexactly (?:what|why|how|the)\b/i,
	],
	[
		'filler phrase',
		/\b(?:in other words|it(?:'s| is) (?:worth noting|important to note)|that said|under the hood|at its core|(?:simply put|put simply)|in short(?!-)|in essence|bottom line|needless to say|when it comes to|at the end of the day|think of (?:it|this) as|no more,? no less|(?:that|which) is to say|here(?:'s| is) (?:why|the thing)|the (?:whole|entire) point|the key (?:insight|takeaway))\b/i,
	],
	[
		'inflated diction',
		/\b(?:leverag(?:e|es|ing)|utiliz(?:e|es|ing)|seamless(?:ly)?|delv(?:e|es|ing)|myriad|plethora|robust|comprehensive(?:ly)?|crucial(?:ly)?|vital(?:ly)?|elegant(?:ly)?|powerful(?:ly)?|intuitive(?:ly)?|nuanced|holistic|granular|meticulous|facilitat(?:e|es|ing)|streamlin(?:e|es|ing)|empower(?:s|ing)?|cutting[-\s]edge|state[-\s]of[-\s]the[-\s]art|arguably|essentially|fundamentally|a wealth of)\b/i,
	],
	[
		'connective glue',
		/\b(?:moreover|furthermore|conversely|as such|it turns out|notably|importantly)\b/i,
	],
	[
		'counterfactual justification',
		/\bso\b[^.]{0,60}\b(?:cannot|can't|could not|never|would)\b|\bwithout\b[^.]{0,70}\bwould\b|\bwould otherwise\b|\botherwise\b[^.]{0,70}\bwould\b|\bso that\b|\b(?:which|that) (?:prevents|keeps|stops)\b/i,
	],
	[
		'paste artifact',
		/[“”‘’]|[\u00A0\u00AD\u200B-\u200D\uFEFF]/,
	],
];

const TOP_LEVEL_DECL = /^(?:package|const|func|type|var)\b/;
const DASH_AS_SUBJECT = /[`'"][—–][`'"]|\b(?:em|en)[-\s]dash|U\+201[34]/i;
const MD_ITEM = /^\s*(?:[-*+]\s|\d+[.)]\s|#{1,6}\s|\||>\s)/;

/** @param {string} path @returns {Lang | null} */
function langFor(path) {
	for (const [pattern, lang] of LANGS) {
		if (pattern.test(path)) return lang;
	}
	return null;
}

/** @param {string} line @param {Lang} lang */
function isCommentLine(line, lang) {
	const text = line.trimStart();
	if (lang === 'hash') return text.startsWith('#') && !text.startsWith('#!');
	return text.startsWith('//')
		|| text.startsWith('/*')
		|| text === '*'
		|| text === '*/'
		|| text.startsWith('* ');
}

/** @param {string} line */
function isGoDirective(line) {
	const directive = /^\/\/go:([a-z][a-z0-9_]*)(?:[ \t]|$)/.exec(line.trimStart());
	if (directive !== null) {
		return directive[1] !== 'generate' || /^\/\/go:generate[ \t]/.test(line);
	}
	return /^\/\/line [^\r\n]+:\d+(?::\d+)?\r?$/.test(line);
}

/** @param {string} patch */
function linesFromPatch(patch) {
	/** @type {string[]} */
	const lines = [];
	let line = 0;
	for (const raw of patch.split('\n')) {
		if (raw.startsWith('@@')) line = Number(/\+(\d+)/.exec(raw)?.[1] ?? 1);
		else if (line > 0 && (raw.startsWith('+') || raw.startsWith(' '))) lines[line++ - 1] = raw.slice(1);
	}
	return lines;
}

/** @param {string} line */
function stripCommentPrefix(line) {
	return line
		.trimStart()
		.replace(/^\/\/\s?/, '')
		.replace(/^\/\*\*?\s?/, '')
		.replace(/^#\s?/, '')
		.replace(/^\*\s?/, '')
		.trim();
}

/** @param {PendingGroup} group @param {string} nextLine @param {string[] | undefined} sourceLines */
function isDocBlock(group, nextLine, sourceLines) {
	if (group.doc) return true;
	if (group.lang !== 'go') return false;
	let firstLine = group.lines[0];
	if (sourceLines !== undefined) {
		let before = group.start - 1;
		let after = group.end;
		while (before > 0 && isCommentLine(sourceLines[before - 1], 'go')) before--;
		while (after < sourceLines.length && isCommentLine(sourceLines[after], 'go')) after++;
		firstLine = sourceLines[before] ?? firstLine;
		nextLine = sourceLines[after] ?? nextLine;
	}
	if (group.topLevel && TOP_LEVEL_DECL.test(nextLine)) return true;
	const field = /^\s+([A-Za-z_]\w*)\s+(?:[A-Za-z_*]|\[)/.exec(nextLine)?.[1];
	return field !== undefined && stripCommentPrefix(firstLine).startsWith(`${field} `);
}

/** @param {PendingGroup} group @param {string} nextLine @param {string[] | undefined} sourceLines */
function reasonsFor(group, nextLine, sourceLines) {
	const reasons = [];
	if (group.lang !== 'md' && group.lines.length >= 3 && !isDocBlock(group, nextLine, sourceLines)) {
		reasons.push(`${group.lines.length} lines`);
	}

	const text = group.lang === 'md'
		? group.lines.join(' ')
		: group.lines.map(stripCommentPrefix).join(' ');
	for (const [name, pattern] of TELLS) {
		if (!pattern.test(text)) continue;
		if (name === 'em dash' && DASH_AS_SUBJECT.test(text)) continue;
		reasons.push(name);
	}
	return reasons;
}

/** @param {Fence} fence @param {string} line @returns {Fence} */
function updateFence(fence, line) {
	const match = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
	if (match === null) return fence;
	const run = match[1];
	const character = run[0];
	if (character !== '`' && character !== '~') return fence;
	if (fence === null) return { character, length: run.length };
	if (character === fence.character && run.length >= fence.length && match[2].trim() === '') return null;
	return fence;
}

/** @param {string} source @param {number} line */
function fenceBefore(source, line) {
	/** @type {Fence} */
	let fence = null;
	for (const content of source.split('\n').slice(0, Math.max(0, line - 1))) {
		fence = updateFence(fence, content);
	}
	return fence;
}

/** @param {string} path @param {string} patch @param {string} [source] @returns {Group[]} */
export function groupsFromPatch(path, patch, source) {
	const lang = langFor(path);
	if (lang === null) return [];
	const markdown = lang === 'md';
	const yaml = /\.ya?ml$/.test(path);
	const sourceLines = source?.split('\n');
	const yamlExamples = yaml ? yamlExampleLines(sourceLines ?? linesFromPatch(patch)) : new Set();
	/** @type {Group[]} */
	const groups = [];
	/** @type {PendingGroup | null} */
	let pending = null;
	let newLine = 0;
	/** @type {Fence} */
	let fence = null;

	/** @param {string} nextLine */
	const flush = nextLine => {
		if (pending !== null) {
			const reasons = reasonsFor(pending, nextLine, sourceLines);
			if (reasons.length > 0) {
				groups.push({
					path,
					start: pending.start,
					end: pending.end,
					text: pending.lines.join('\n'),
					reasons,
				});
			}
		}
		pending = null;
	};

	/** @param {string} content */
	const startsBlock = content => {
		if (!markdown) {
			return isCommentLine(content, lang)
				&& !(lang === 'go' && isGoDirective(content))
				&& !yamlExamples.has(newLine);
		}
		const nextFence = updateFence(fence, content);
		if (nextFence !== fence) {
			fence = nextFence;
			return false;
		}
		return fence === null && content.trim() !== '' && !/^(?: {4}|\t)/.test(content);
	};

	for (const raw of patch.split('\n')) {
		if (raw.startsWith('@@')) {
			flush('');
			const match = /\+(\d+)/.exec(raw);
			newLine = match === null ? 1 : Number.parseInt(match[1], 10);
			fence = markdown && source !== undefined ? fenceBefore(source, newLine) : null;
			continue;
		}

		if (raw.startsWith('+')) {
			const content = raw.slice(1);
			if (startsBlock(content)) {
				const text = content.trimStart();
				if (pending !== null && (markdown ? MD_ITEM.test(content) : text.startsWith('/**'))) {
					flush(content);
				}
				if (pending === null) {
					pending = {
						start: newLine,
						end: newLine,
						lang,
						topLevel: !/^[\t ]/.test(content),
						doc: text.startsWith('/**'),
						lines: [content],
					};
				} else {
					pending.end = newLine;
					pending.lines.push(content);
				}
			} else {
				flush(content);
			}
			newLine++;
			continue;
		}

		if (raw.startsWith('-')) {
			flush('');
			continue;
		}
		if (raw.startsWith('\\')) continue;

		const content = raw.startsWith(' ') ? raw.slice(1) : raw;
		if (markdown) fence = updateFence(fence, content);
		flush(content);
		newLine++;
	}

	flush('');
	return groups;
}

if (process.env.NODE_TEST_CONTEXT) {
	const { test } = await import('node:test');
	/** @type {typeof import('node:assert').strict} */
	const assert = (await import('node:assert')).strict;

	/** @param {string[]} lines @param {number} [start] @param {number} [from] @param {number} [to] */
	const hunk = (lines, start = 1, from = start, to = lines.length) =>
		[
			`@@ -1 +${start} @@`,
			...lines.slice(start - 1).map((line, index) => {
				const number = start + index;
				return `${number >= from && number <= to ? '+' : ' '}${line}`;
			}),
		].join('\n');
	/** @param {string} path @param {string} patch @param {string[]} [source] */
	const reasons = (path, patch, source) => groupsFromPatch(path, patch, source?.join('\n')).map(group => group.reasons);

	test('flags a long implementation comment', () => {
		const lines = [
			'\t// Parse the value here.',
			'\t// Keep the original around.',
			'\t// Return both values.',
			'\tparse(value)',
		];
		assert.deepEqual(reasons('rule.go', hunk(lines)), [['3 lines']]);
	});

	test('flags style tells in a one-line comment', () => {
		for (
			const [comment, ...expected] of [
				['// Use the cache rather than parsing twice.', '"X rather than Y"'],
				['// In short, renew the token.', 'filler phrase'],
				['// Keep tokens in short-lived storage.'],
				['// Renew the token — then retry.', 'em dash'],
				["// Split fields on the '—' character."],
			]
		) {
			assert.deepEqual(reasons('rule.go', hunk([comment])).flat(), expected, comment);
		}
	});

	test('Go directives split comment groups', () => {
		/** @param {string} path @param {string} first */
		const split = (path, first) => reasons(path, hunk([first, '// Read the input.', '// Preserve its range.']));
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
			assert.deepEqual(split('generate.go', directive), [], directive);
		}
		for (
			const prose of [
				'// go:generate source files.',
				'//go: generate source files.',
				'//go:generate',
				'\t//go:generate go run generator.go',
				'//line generated.go:unknown',
				'\t//line generated.go:12',
			]
		) {
			assert.deepEqual(split('generate.go', prose), [['3 lines']], prose);
		}
		assert.deepEqual(split('generate.js', '//go:embed assets/*'), [['3 lines']]);
	});

	test('directive-adjacent prose retains style and length findings', () => {
		const lines = [
			'//go:generate echo "robust rather than elegant"',
			'// Use the cache rather than parsing twice.',
			'//go:generate go run generator.go',
			'// Parse the value.',
			'// Keep its position.',
			'// Return its diagnostics.',
			'//go:generate gofmt generated.go',
		];
		for (const patch of [hunk(lines), hunk(lines, 1, 2)]) {
			for (const source of [undefined, lines.join('\n')]) {
				assert.deepEqual(
					groupsFromPatch('generate.go', patch, source).map(({ start, end, reasons }) => ({ start, end, reasons })),
					[{ start: 2, end: 2, reasons: ['"X rather than Y"'] }, { start: 4, end: 6, reasons: ['3 lines'] }],
				);
			}
		}
	});

	test('does not measure Go docs by length, reading their context from the full file', () => {
		const parse = [
			'// Parse reads the input.',
			'// It keeps positions.',
			'// It returns diagnostics.',
			'//go:noinline',
		];
		const check = [
			'// Check scans expressions.',
			'// It keeps positions.',
			'// It reports contexts.',
			'// It stops early.',
		];
		const metadata = [
			'type Metadata struct {',
			'\t// Defaults holds input values.',
			'\t// Each value includes its position.',
			'\t// Values remain in source order.',
			'\t// The checker reads these values.',
			'\tDefaults []*Value',
			'}',
		];
		for (
			const { lines, start, from, to } of [
				{ lines: [...parse, 'func Parse() {}'], start: 1, from: 1, to: 3 },
				{ lines: [...check, 'func Check() {}'], start: 1, from: 1, to: 3 },
				{ lines: metadata, start: 2, from: 3, to: 5 },
			]
		) {
			assert.deepEqual(reasons('rule.go', hunk(lines), lines), []);
			assert.deepEqual(reasons('rule.go', hunk(lines, start, from, to), lines), []);
			assert.deepEqual(reasons('rule.go', hunk(lines, start, from, to)), [['3 lines']]);
		}
	});

	test('scans Markdown prose but skips fenced code', () => {
		const lines = ['That said, this is prose.', '', '```go', '// Robust code.', '```', '', '- Moreover, an item.'];
		assert.deepEqual(reasons('docs/checks.md', hunk(lines)), [['filler phrase'], ['connective glue']]);
	});

	test('restores Markdown fence state before each diff hunk', () => {
		const lines = ['````markdown', 'This contains a shorter ``` marker.', 'That said, this is fence content.', '````'];
		assert.deepEqual(reasons('docs/checks.md', hunk(lines, 3, 3, 3), lines), []);
	});

	test('classifies YAML lines in a partial hunk from the full file', () => {
		for (
			const lines of [
				['# run: |', '#   echo use this rather than that'],
				['# tools:', '#   ruff:', '#     args: --fix rather than check'],
			]
		) {
			const patch = hunk(lines, lines.length);
			assert.deepEqual(reasons('workflow.yml', patch, lines), []);
			assert.deepEqual(reasons('workflow.yml', patch), [['"X rather than Y"']]);
		}
	});

	test('finds nothing in the commented examples of config_default.yaml', () => {
		const path = new URL('../../../internal/input/configuration/config_default.yaml', import.meta.url);
		const lines = readFileSync(path, 'utf8').split('\n');
		const examples = lines.findIndex(line => line.startsWith('# Plugins check scripts')) + 1;
		assert.ok(examples > 0);
		const groups = groupsFromPatch('config_default.yaml', hunk(lines), lines.join('\n'));
		assert.deepEqual(groups.filter(group => group.start >= examples), []);
	});
}

/** @param {Group} group */
export const keyFor = group =>
	createHash('sha256')
		.update(group.path)
		.update('\0')
		.update(String(group.start))
		.update('\0')
		.update(group.text)
		.digest('hex')
		.slice(0, 16);

/** @type {'RIGHT'} */
const RIGHT = 'RIGHT';

/** @param {Group} group */
export function bodyFor(group) {
	return `<!-- actionlint-comment-cop:${keyFor(group)} -->\n`
		+ `Flagged for: ${group.reasons.join(', ')}.\n\n`
		+ '[Writing instructions](https://github.com/kjanat/actionlint/blob/master/.agents/writing.md)';
}

if (process.env.NODE_TEST_CONTEXT) {
	const { test } = await import('node:test');
	/** @type {typeof import('node:assert').strict} */
	const assert = (await import('node:assert')).strict;
	const { existsSync } = await import('node:fs');

	test('gives identical text on different lines different marker keys', () => {
		const group = { path: 'docs/notes.md', start: 1, end: 1, text: 'That said, prose.', reasons: [] };
		assert.notEqual(keyFor(group), keyFor({ ...group, start: 10, end: 10 }));
	});

	test('writes the marker, the reasons and a link to the writing instructions', () => {
		const group = { path: 'rule.go', start: 1, end: 3, text: '// Text', reasons: ['3 lines', '"X instead of Y"'] };
		const link = 'https://github.com/kjanat/actionlint/blob/master/.agents/writing.md';
		assert.equal(
			bodyFor(group),
			`<!-- actionlint-comment-cop:${
				keyFor(group)
			} -->\nFlagged for: 3 lines, "X instead of Y".\n\n[Writing instructions](${link})`,
		);
		assert.ok(existsSync(new URL(`../../../${link.split('/blob/master/')[1]}`, import.meta.url)));
	});
}

/** @param {unknown} error */
function errorMessage(error) {
	return error instanceof Error ? error.message : String(error);
}

/** @param {unknown} value @param {string} name */
function requiredString(value, name) {
	if (typeof value !== 'string' || value === '') throw new Error(`${name} is required`);
	return value;
}

/** @param {RunArguments} args */
export default async function run({ github, context, core, files }) {
	const pullRequest = context.payload.pull_request;
	if (pullRequest === undefined) throw new Error('pull_request payload is required');

	const owner = context.repo.owner;
	const repo = context.repo.repo;
	const pullNumber = pullRequest.number;
	const headSha = requiredString(pullRequest.head.sha, 'pull request head SHA');
	/** @type {Group[]} */
	const groups = [];
	for (const file of files) {
		groups.push(...groupsFromPatch(file.filename, file.patch, file.source));
	}

	const presentKeys = new Set(groups.map(keyFor));
	const seenKeys = new Set();
	/** @type {string[]} */
	const staleThreadIds = [];
	let after = null;
	for (;;) {
		/** @type {ReviewThreadsResponse} */
		const response = await github.graphql(
			`query($owner: String!, $repo: String!, $pr: Int!, $after: String) {
				repository(owner: $owner, name: $repo) {
					pullRequest(number: $pr) {
						reviewThreads(first: 100, after: $after) {
							pageInfo { hasNextPage endCursor }
							nodes { id isResolved path comments(first: 1) { nodes { body viewerDidAuthor } } }
						}
					}
				}
			}`,
			{ owner, repo, pr: pullNumber, after },
		);
		const page = response.repository.pullRequest.reviewThreads;
		for (const thread of page.nodes) {
			const comment = thread.comments.nodes[0];
			if (comment === undefined || !comment.viewerDidAuthor) continue;
			const match = /<!-- actionlint-comment-cop:([a-f0-9]+) -->/.exec(comment.body);
			if (match === null) continue;
			const key = match[1];
			seenKeys.add(key);
			if (!thread.isResolved && !presentKeys.has(key)) {
				staleThreadIds.push(thread.id);
			}
		}
		if (!page.pageInfo.hasNextPage) break;
		after = page.pageInfo.endCursor;
	}

	let resolved = 0;
	for (const id of staleThreadIds) {
		try {
			await github.graphql(
				`mutation($id: ID!) {
					resolveReviewThread(input: {threadId: $id}) { thread { id } }
				}`,
				{ id },
			);
			resolved++;
		} catch (error) {
			core.warning(`Could not resolve Comment Cop thread ${id}: ${errorMessage(error)}`);
		}
	}

	const comments = groups.filter(group => !seenKeys.has(keyFor(group))).map(group => ({
		path: group.path,
		line: group.end,
		side: RIGHT,
		body: bodyFor(group),
		...(group.start < group.end
			? { start_line: group.start, start_side: RIGHT }
			: {}),
	}));
	let posted = 0;
	if (comments.length > 0) {
		try {
			await github.rest.pulls.createReview({
				owner,
				repo,
				pull_number: pullNumber,
				commit_id: headSha,
				event: 'COMMENT',
				body: 'Please review the flagged wording in the inline comments.',
				comments,
			});
			posted = comments.length;
		} catch (error) {
			core.warning(`Could not submit Comment Cop review: ${errorMessage(error)}`);
		}
	}

	core.info(`Comment Cop: ${posted} posted, ${resolved} stale threads resolved, ${groups.length} present.`);
}

if (process.env.NODE_TEST_CONTEXT) {
	const { test, mock } = await import('node:test');
	/** @type {typeof import('node:assert').strict} */
	const assert = (await import('node:assert')).strict;

	/** @param {string} filename @param {string} source @returns {ChangedFile} */
	const added = (filename, source) => ({
		filename,
		source,
		patch: `@@ -1 +1 @@\n${source.split('\n').map(line => `+${line}`).join('\n')}`,
	});
	const files = [
		added(
			'docs/first.md',
			'That said, the cache stores values.\nRead the cached entry.\n\n\nMoreover, refresh the entry.',
		),
		added('docs/second.md', 'Use the cache rather than fetching again.'),
	];
	/** @type {Group[]} */
	const findings = [
		{ path: files[0].filename, start: 1, end: 2, text: files[0].source.split('\n\n')[0], reasons: ['filler phrase'] },
		{ path: files[0].filename, start: 5, end: 5, text: 'Moreover, refresh the entry.', reasons: ['connective glue'] },
		{ path: files[1].filename, start: 1, end: 1, text: files[1].source, reasons: ['"X rather than Y"'] },
	];

	/** @param {ChangedFile[]} changed @param {Array<{body: string, viewerDidAuthor?: boolean, isResolved?: boolean}>} threads */
	const runWith = async (changed, threads) => {
		const nodes = threads.map(({ body, viewerDidAuthor = true, isResolved = false }, index) => ({
			id: `thread-${index}`,
			isResolved,
			path: '',
			comments: { nodes: [{ body, viewerDidAuthor }] },
		}));
		const page = { pageInfo: { hasNextPage: false, endCursor: null }, nodes };
		const graphql = mock.fn(async (/** @type {string} */ _query, /** @type {object} */ _parameters) => ({
			repository: { pullRequest: { reviewThreads: page } },
		}));
		const createReview = mock.fn(async (/** @type {ReviewParams} */ params) => params);
		const info = mock.fn((/** @type {string} */ _message) => {});
		await run({
			files: changed,
			github: { graphql, rest: { pulls: { createReview } } },
			context: {
				repo: { owner: 'owner', repo: 'repo' },
				payload: { pull_request: { number: 42, head: { sha: 'a'.repeat(40) } } },
			},
			core: { info, warning: mock.fn() },
		});
		return {
			reviews: createReview.mock.calls.map(call => call.arguments[0]),
			resolved: graphql.mock.calls.slice(1).map(call => call.arguments[1]),
			summary: info.mock.calls.map(call => call.arguments[0]),
		};
	};

	test('submits every finding in one review', async () => {
		const { reviews, summary } = await runWith(files, []);
		assert.deepEqual(reviews, [{
			owner: 'owner',
			repo: 'repo',
			pull_number: 42,
			commit_id: 'a'.repeat(40),
			event: 'COMMENT',
			body: 'Please review the flagged wording in the inline comments.',
			comments: [
				{
					path: 'docs/first.md',
					line: 2,
					side: 'RIGHT',
					body: bodyFor(findings[0]),
					start_line: 1,
					start_side: 'RIGHT',
				},
				{ path: 'docs/first.md', line: 5, side: 'RIGHT', body: bodyFor(findings[1]) },
				{ path: 'docs/second.md', line: 1, side: 'RIGHT', body: bodyFor(findings[2]) },
			],
		}]);
		assert.deepEqual(summary, ['Comment Cop: 3 posted, 0 stale threads resolved, 3 present.']);
	});

	test('does not repeat findings that already have a thread', async () => {
		const partial = await runWith(files, [{ body: bodyFor(findings[0]) }]);
		assert.deepEqual(partial.reviews.map(review => review?.comments?.map(comment => comment.line)), [[5, 1]]);
		const complete = await runWith(files, findings.map(finding => ({ body: bodyFor(finding) })));
		assert.deepEqual([complete.reviews, complete.resolved], [[], []]);
	});

	test('resolves threads whose finding is gone', async () => {
		const { reviews, resolved, summary } = await runWith([], [{ body: bodyFor(findings[0]) }]);
		assert.deepEqual([reviews, resolved], [[], [{ id: 'thread-0' }]]);
		assert.deepEqual(summary, ['Comment Cop: 0 posted, 1 stale threads resolved, 0 present.']);
	});

	test('ignores threads that are foreign, resolved or unmarked', async () => {
		const gone = bodyFor({ ...findings[0], start: 9 });
		const { resolved, summary } = await runWith(files, [
			{ body: bodyFor(findings[0]), viewerDidAuthor: false },
			{ body: gone, viewerDidAuthor: false },
			{ body: gone, isResolved: true },
			{ body: 'Please reword this.' },
		]);
		assert.deepEqual(resolved, []);
		assert.deepEqual(summary, ['Comment Cop: 3 posted, 0 stale threads resolved, 3 present.']);
	});
}

const git = (/** @type {string[]} */ args) => execFileSync('git', args, { encoding: 'utf8', maxBuffer: 1 << 28 });

/** @param {number} descriptor */
function* readPatches(descriptor) {
	const decoder = new StringDecoder('utf8');
	const buffer = Buffer.alloc(64 * 1_024);
	const separator = '\ndiff --git ';
	/** @type {string[]} */
	let parts = [];
	let pending = '';
	let position = 0;
	for (;;) {
		const count = readSync(descriptor, buffer, 0, buffer.length, position);
		position += count;
		pending += count === 0 ? decoder.end() : decoder.write(buffer.subarray(0, count));
		let boundary = pending.indexOf(separator);
		while (boundary !== -1) {
			parts.push(pending.slice(0, boundary + 1));
			yield parts.join('');
			parts = [];
			pending = pending.slice(boundary + 1);
			boundary = pending.indexOf(separator);
		}
		if (count === 0) break;
		if (pending.length > separator.length) {
			parts.push(pending.slice(0, -separator.length));
			pending = pending.slice(-separator.length);
		}
	}
	parts.push(pending);
	const patch = parts.join('');
	if (patch !== '') yield patch;
}

/** @param {number} descriptor @param {number} size @param {number} position */
function readBytes(descriptor, size, position) {
	const buffer = Buffer.alloc(size);
	let offset = 0;
	while (offset < size) {
		const count = readSync(descriptor, buffer, offset, size - offset, position + offset);
		if (count === 0) throw new Error('Incomplete Git blob');
		offset += count;
	}
	return buffer;
}

/** @param {string} name */
const scannable = name =>
	langFor(name) !== null && !name.startsWith('vendor/') && !name.includes('/vendor/')
	&& !name.startsWith('docs/releases/');

/**
 * @param {{cacheDirectory: string, repositoryUrl: string, baseSha: string, headSha: string, token?: string, maxBuffer?: number}} options
 * @returns {ChangedFile[]}
 */
export function readPullRequestFiles({ cacheDirectory, repositoryUrl, baseSha, headSha, token, maxBuffer = 1 << 28 }) {
	for (const sha of [baseSha, headSha]) {
		if (!/^[a-f0-9]{40}$/.test(sha)) throw new Error('Expected a full Git commit SHA');
	}
	mkdirSync(cacheDirectory, { recursive: true });
	const repository = join(cacheDirectory, 'repository.git');
	/** @type {NodeJS.ProcessEnv} */
	const environment = { ...process.env, GIT_TERMINAL_PROMPT: '0', GIT_NO_REPLACE_OBJECTS: '1' };
	if (token) {
		const url = new URL(repositoryUrl);
		if (url.protocol !== 'https:') throw new Error('Git authentication requires HTTPS');
		const index = Number(environment.GIT_CONFIG_COUNT ?? 0);
		environment.GIT_CONFIG_COUNT = String(index + 1);
		environment[`GIT_CONFIG_KEY_${index}`] = `http.${url.origin}/.extraheader`;
		environment[`GIT_CONFIG_VALUE_${index}`] = `AUTHORIZATION: basic ${
			Buffer.from(`x-access-token:${token}`).toString('base64')
		}`;
	}
	/** @param {string[]} args @param {string} [input] @param {number | 'pipe'} [output] */
	const execute = (args, input, output = 'pipe') =>
		execFileSync('git', args, {
			maxBuffer,
			timeout: 60_000,
			input,
			env: environment,
			stdio: ['pipe', output, 'pipe'],
		});
	execute(['init', '--bare', repository]);
	/** @param {string[]} args */
	const read = args => execute(['--git-dir', repository, ...args]).toString('utf8');
	read([
		'fetch',
		'--no-tags',
		'--no-recurse-submodules',
		repositoryUrl,
		`+${baseSha}:refs/comment-cop/base`,
		`+${headSha}:refs/comment-cop/head`,
	]);
	const base = read(['merge-base', baseSha, headSha]).trim();
	const diff = ['diff', '--no-ext-diff', '--no-textconv', '--no-renames', '--diff-filter=AM'];
	const records = read([...diff, '--raw', '--abbrev=40', '-z', base, headSha, '--']).split('\0');
	records.pop();
	/** @type {Array<{filename: string, patch: string, object: string}>} */
	const files = [];
	for (let index = 0; index < records.length; index += 2) {
		const filename = records[index + 1];
		if (!scannable(filename)) continue;
		const object = /^:\d+ \d+ [a-f0-9]{40} ([a-f0-9]{40}) [AM]$/.exec(records[index])?.[1];
		if (object === undefined) throw new Error('Invalid Git diff record');
		files.push({ filename, patch: '', object });
	}
	if (files.length === 0) return [];
	const temporary = mkdtempSync(join(cacheDirectory, 'read-'));
	/**
	 * @template T
	 * @param {string[]} args
	 * @param {string | undefined} input
	 * @param {(descriptor: number) => T} consume
	 */
	const capture = (args, input, consume) => {
		const descriptor = openSync(join(temporary, 'output'), 'w+');
		try {
			execute(['--git-dir', repository, '--literal-pathspecs', ...args], input, descriptor);
			return consume(descriptor);
		} finally {
			closeSync(descriptor);
		}
	};
	try {
		for (let start = 0; start < files.length;) {
			let end = start;
			let argumentBytes = 0;
			while (end < files.length) {
				const bytes = Buffer.byteLength(files[end].filename) * 2 + 3;
				if (end > start && argumentBytes + bytes > 16 * 1_024) break;
				argumentBytes += bytes;
				end++;
			}
			const batch = files.slice(start, end);
			capture(
				[...diff, '--patch', '-U3', base, headSha, '--', ...batch.map(file => file.filename)],
				undefined,
				descriptor => {
					let index = 0;
					for (const patch of readPatches(descriptor)) {
						if (index === batch.length) throw new Error('Git diff records and patches disagree');
						batch[index++].patch = patch;
					}
					if (index !== batch.length) throw new Error('Git diff records and patches disagree');
				},
			);
			start = end;
		}
		return capture(['cat-file', '--batch'], `${files.map(file => file.object).join('\n')}\n`, descriptor => {
			const length = fstatSync(descriptor).size;
			let offset = 0;
			return files.map(({ filename, patch, object }) => {
				const header = readBytes(descriptor, Math.min(96, length - offset), offset);
				const newline = header.indexOf(10);
				const match = /^([a-f0-9]{40}) blob (\d+)$/.exec(header.toString('ascii', 0, newline));
				if (newline < 0 || match === null || match[1] !== object) throw new Error('Invalid Git blob header');
				const size = Number(match[2]);
				const start = offset + newline + 1;
				const end = start + size;
				if (!Number.isSafeInteger(end) || end >= length || readBytes(descriptor, 1, end)[0] !== 10) {
					throw new Error('Incomplete Git blob');
				}
				offset = end + 1;
				return { filename, patch, source: readBytes(descriptor, size, start).toString('utf8') };
			});
		});
	} finally {
		rmSync(temporary, { recursive: true, force: true });
	}
}

if (process.env.NODE_TEST_CONTEXT) {
	const { test } = await import('node:test');
	/** @type {typeof import('node:assert').strict} */
	const assert = (await import('node:assert')).strict;
	const { readdirSync, writeFileSync } = await import('node:fs');
	const { tmpdir } = await import('node:os');

	/** @param {import('node:test').TestContext} t */
	const repository = t => {
		const directory = mkdtempSync(join(tmpdir(), 'comment-cop-'));
		t.after(() => rmSync(directory, { recursive: true, force: true }));
		const origin = join(directory, 'origin');
		mkdirSync(origin);
		/** @param {string[]} args */
		const git = args =>
			execFileSync('git', ['-C', origin, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
		git(['init', '--quiet', '--initial-branch=master', '--template=']);
		for (
			const [key, value] of [
				['user.name', 'Test'],
				['user.email', 'test@example.invalid'],
				['commit.gpgsign', 'false'],
				['core.hooksPath', join(directory, 'no-hooks')],
			]
		) {
			git(['config', key, value]);
		}
		/** @param {Record<string, string>} files */
		const commit = files => {
			for (const [name, source] of Object.entries(files)) writeFileSync(join(origin, name), source);
			git(['add', '--all']);
			git(['commit', '--quiet', '--allow-empty', '--message', 'Change']);
			return git(['rev-parse', 'HEAD']);
		};
		/** @param {string} baseSha @param {string} headSha @param {number} [maxBuffer] */
		const read = (baseSha, headSha, maxBuffer) =>
			readPullRequestFiles({
				cacheDirectory: join(directory, 'cache'),
				repositoryUrl: origin,
				baseSha,
				headSha,
				maxBuffer,
			});
		return { directory, git, commit, read };
	};

	test('skips unsupported files, vendored code and release notes', () => {
		const paths = ['docs/checks.md', 'notes.txt', 'vendor/a.go', 'internal/vendor/a.go', 'docs/releases/v1.md'];
		assert.deepEqual(paths.filter(scannable), ['docs/checks.md']);
	});

	test('reads a fork diff through cached Git objects without checking out PR code', t => {
		const { directory, git, commit, read } = repository(t);
		const ancestor = commit({ 'shared.md': 'Initial text.\n' });
		const baseSha = commit({ 'base-only.md': 'Base branch update.\n' });
		git(['checkout', '--quiet', '-b', 'contribution', ancestor]);
		const filename = '[special] with spaces.md';
		const source = '```go\n// That said, example code is excluded.\n```\n\nThat said, scan this prose.\n';
		const first = read(baseSha, commit({ [filename]: source }));
		assert.deepEqual(first.map(file => [file.filename, file.source]), [[filename, source]]);
		assert.deepEqual(groupsFromPatch(filename, first[0].patch, source).map(group => group.start), [5]);
		assert.deepEqual(read(baseSha, git(['rev-parse', 'HEAD'])), first);
		const objects = join(directory, 'cache', 'repository.git');
		assert.equal(git(['--git-dir', objects, 'rev-parse', '--is-bare-repository']), 'true');
		assert.doesNotMatch(readFileSync(join(objects, 'config'), 'utf8'), /extraheader|token|\[remote/);
		git(['rm', '--quiet', filename]);
		const updated = read(baseSha, commit({ 'replacement.md': 'Moreover, scan the replacement.\n' }));
		assert.deepEqual(updated.map(file => file.filename), ['replacement.md']);
	});

	test('rejects invalid Git revisions before fetching', () => {
		const options = {
			cacheDirectory: 'unused',
			repositoryUrl: 'unused',
			baseSha: '--upload-pack=bad',
			headSha: 'a'.repeat(40),
		};
		assert.throws(() => readPullRequestFiles(options), /full Git commit SHA/);
	});

	test('reads every source and patch in a large diff with Unicode and binary files', t => {
		const { commit, read } = repository(t);
		const baseSha = commit({});
		/** @type {Record<string, string>} */
		const expected = { '[binary] with spaces.md': 'Binary\0content\n' };
		for (let index = 0; index < 1_000; index++) {
			expected[`file-${String(index).padStart(4, '0')}.md`] =
				`That said, café 😀 ${index}.\n\ndiff --git this is prose\n`;
		}
		const files = read(baseSha, commit({ ...expected, 'unsupported.txt': 'Not a scanned file.' }));
		assert.deepEqual(Object.fromEntries(files.map(file => [file.filename, file.source])), expected);
		for (const { filename, patch, source } of files.filter(file => file.filename.startsWith('file-'))) {
			assert.deepEqual(groupsFromPatch(filename, patch, source).map(group => group.start), [1]);
		}
	});

	const line = 'That said, café 😀.\n';
	/** @param {string} first */
	const blobs = first => Object.fromEntries([0, 1, 2, 3].map(index => [`file-${index}.md`, first + line.repeat(399)]));
	/** @type {Array<[string, Record<string, string>, Record<string, string>]>} */
	const scenarios = [
		['unsupported patch', {}, { 'small.md': line, 'unsupported.txt': line.repeat(4_000) }],
		['combined blobs', blobs('Old text.\n'), blobs(line)],
		['single patch and blob', {}, { 'large.md': line.repeat(4_000) }],
	];
	for (const [scenario, base, head] of scenarios) {
		test(`reads supported files beyond the subprocess buffer limit: ${scenario}`, t => {
			const { directory, commit, read } = repository(t);
			const files = read(commit(base), commit(head), 16 * 1_024);
			const supported = Object.entries(head).filter(([name]) => name.endsWith('.md'));
			assert.deepEqual(files.map(({ filename, source }) => [filename, source]), supported);
			for (const { filename, patch, source } of files) {
				const before = (base[filename] ?? '').split('\n');
				assert.deepEqual(
					patch.split('\n').filter(row => row.startsWith('+') && !row.startsWith('+++')).map(row => row.slice(1)),
					source.trimEnd().split('\n').filter((row, index) => row !== before[index]),
				);
			}
			assert.deepEqual(readdirSync(join(directory, 'cache')), ['repository.git']);
		});
	}
}

function defaultBase() {
	for (const ref of ['origin/master', 'master']) {
		try {
			return execFileSync('git', ['merge-base', 'HEAD', ref], {
				encoding: 'utf8',
				stdio: ['ignore', 'pipe', 'pipe'],
			}).trim();
		} catch {}
	}
	throw new Error('no origin/master or master found; pass a base ref');
}

/** @param {string} name */
function untrackedPatch(name) {
	try {
		return git(['diff', '--no-index', '--', '/dev/null', name]);
	} catch (error) {
		if (error instanceof Error && 'stdout' in error && typeof error.stdout === 'string') {
			return error.stdout;
		}
		return '';
	}
}

/** @param {string} output */
const outputLines = output => output.split('\n').map(line => line.trim()).filter(Boolean);

/** @param {string | undefined} base */
export function scanLocal(base) {
	const from = base ?? defaultBase();
	const tracked = outputLines(git(['diff', '--name-only', '--diff-filter=d', from, '--'])).filter(scannable);
	const untracked = outputLines(git(['ls-files', '--others', '--exclude-standard'])).filter(scannable);
	const groups = [
		...tracked.flatMap(name =>
			groupsFromPatch(name, git(['diff', '-U3', from, '--', name]), readFileSync(name, 'utf8'))
		),
		...untracked.flatMap(name => groupsFromPatch(name, untrackedPatch(name), readFileSync(name, 'utf8'))),
	];

	for (const group of groups) {
		console.log(`${group.path}:${group.start}-${group.end} [${group.reasons.join(', ')}]`);
		console.log(`${group.text}\n`);
	}
	console.log(`comment-cop: ${groups.length === 0 ? 'clean' : `${groups.length} finding(s)`}.`);
	return groups.length === 0 ? 0 : 1;
}

if (import.meta.main && !process.env.NODE_TEST_CONTEXT) {
	const args = process.argv.slice(2);
	if (args.length > 1 || args.includes('--help') || args.includes('-h')) {
		console.log('usage: comment-cop.mjs [<base-ref>]');
		process.exitCode = args.length > 1 ? 2 : 0;
	} else {
		try {
			process.exitCode = scanLocal(args[0]);
		} catch (error) {
			console.error(errorMessage(error));
			process.exitCode = 2;
		}
	}
}
