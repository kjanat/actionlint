import { createHash } from 'node:crypto';
import { readFile, realpath } from 'node:fs/promises';
import { isAbsolute, relative, resolve, sep } from 'node:path';

import type { ActionResult, Diagnostic, Edit, Fix, Position } from '#result';
import { object } from '#result';
import type { Environment } from '#runtime';

type Hunk = { start: number; end: number; added: Set<number> };
type ReviewComment = {
	path: string;
	line: number;
	side: 'RIGHT';
	body: string;
	start_line?: number;
	start_side?: 'RIGHT';
};
type ReviewContext = { repository: string; sourceRepository: string; number: number; sha: string };

export type ReviewRuntime = {
	request: typeof fetch;
	readSource: (path: string) => Promise<string>;
};

function repository(value: unknown): value is string {
	return typeof value === 'string' && /^[\w.-]+\/[\w.-]+$/.test(value);
}

function context(value: unknown, target: string | undefined): ReviewContext {
	if (!object(value) || !object(value.pull_request) || !object(value.pull_request.head)) {
		throw new Error('this event does not identify a pull request');
	}
	const { head } = value.pull_request;
	if (
		!repository(target) || !object(head.repo) || !repository(head.repo.full_name)
		|| typeof head.sha !== 'string' || !/^[a-f0-9]{40}$/.test(head.sha)
		|| typeof value.number !== 'number' || !Number.isSafeInteger(value.number) || value.number < 1
	) throw new Error('the pull request event has no valid repository, number or head SHA');
	return { repository: target, sourceRepository: head.repo.full_name, sha: head.sha, number: value.number };
}

export function diffHunks(patch: string): Hunk[] {
	const hunks: Hunk[] = [];
	let current: Hunk | undefined;
	let line = 0;
	for (const text of patch.split('\n')) {
		const header = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/.exec(text);
		if (header) {
			line = Number(header[1]);
			const count = header[2] === undefined ? 1 : Number(header[2]);
			current = { start: line, end: line + count - 1, added: new Set() };
			hunks.push(current);
		} else if (current && text.startsWith('+')) {
			current.added.add(line++);
		} else if (current && text.startsWith(' ')) {
			line++;
		}
	}
	return hunks;
}

function inDiff(hunks: Hunk[], start: number, end: number): boolean {
	return hunks.some((hunk) =>
		hunk.start <= start && end <= hunk.end && [...hunk.added].some((line) => line >= start && line <= end)
	);
}

function markdownCode(text: string, language = ''): string {
	const lengths = [...text.matchAll(/`+/g)].map((match) => match[0].length);
	const fence = '`'.repeat(Math.max(3, ...lengths.map((length) => length + 1)));
	return `${fence}${language}\n${text}\n${fence}`;
}

function offset(lines: string[], position: Position): number | undefined {
	const line = lines[position.line - 1];
	if (line === undefined || position.column < 1) return undefined;
	const characters = [...line];
	if (position.column > characters.length + 1) return undefined;
	return lines.slice(0, position.line - 1).reduce((length, previous) => length + previous.length + 1, 0)
		+ characters.slice(0, position.column - 1).join('').length;
}

// A single suggestion applies the entire fix. Partial or overlapping fix groups are omitted.
export function suggestion(
	fix: Fix,
	path: string,
	source: string,
): { start: number; end: number; replacement: string } | undefined {
	if (fix.edits.length === 0 || fix.edits.some((edit) => edit.path !== path)) return undefined;
	const lines = source.replaceAll('\r\n', '\n').split('\n');
	const edits: { start: number; end: number; replacement: string }[] = [];
	let firstLine = Number.MAX_SAFE_INTEGER;
	let lastLine = 0;
	for (const edit of fix.edits) {
		const start = offset(lines, edit.start);
		const end = offset(lines, edit.end);
		if (start === undefined || end === undefined || start > end) return undefined;
		firstLine = Math.min(firstLine, edit.start.line);
		lastLine = Math.max(
			lastLine,
			edit.end.line > edit.start.line && edit.end.column === 1 ? edit.end.line - 1 : edit.end.line,
		);
		edits.push({ start, end, replacement: edit.replacement });
	}
	if (lastLine - firstLine > 40) return undefined;
	edits.sort((left, right) => left.start - right.start || left.end - right.end);
	for (let index = 1; index < edits.length; index++) {
		const previous = edits[index - 1];
		const next = edits[index];
		if (!previous || !next || previous.end > next.start || previous.start === next.start) return undefined;
	}
	const start = offset(lines, { line: firstLine, column: 1 });
	const last = lines[lastLine - 1];
	if (start === undefined || last === undefined) return undefined;
	const end = offset(lines, { line: lastLine, column: [...last].length + 1 });
	if (end === undefined) return undefined;
	let replacement = lines.join('\n').slice(start, end);
	for (const edit of edits.toReversed()) {
		// This suggestion ends before the last line's newline. Reject edits that
		// extend beyond that boundary.
		if (edit.end > end) return undefined;
		replacement = replacement.slice(0, edit.start - start) + edit.replacement + replacement.slice(edit.end - start);
	}
	if (replacement.length > 10_000) return undefined;
	return { start: firstLine, end: lastLine, replacement };
}

export function reviewComment(
	diagnostic: Diagnostic,
	path: string,
	source: string,
	hunks: Hunk[],
	sha: string,
	includeSuggestion = true,
): ReviewComment | undefined {
	let start = diagnostic.start.line;
	let end = diagnostic.end.line > start && diagnostic.end.column === 1 ? diagnostic.end.line - 1 : diagnostic.end.line;
	let body = markdownCode(`${diagnostic.code || diagnostic.rule}: ${diagnostic.message}`);
	const fix = diagnostic.fixes?.[0];
	const replacement = fix && suggestion(fix, diagnostic.path, source);
	if (replacement && inDiff(hunks, replacement.start, replacement.end)) {
		start = replacement.start;
		end = replacement.end;
		if (includeSuggestion) body += `\n\n${markdownCode(replacement.replacement, 'suggestion')}`;
	} else if (!inDiff(hunks, start, end)) {
		return undefined;
	}
	const digest = createHash('sha256').update(JSON.stringify({ sha, path, start, end, body })).digest('hex');
	body += `\n\n<!-- actionlint:${sha}:${digest} -->`;
	const comment: ReviewComment = { path, line: end, side: 'RIGHT', body };
	if (start !== end) {
		comment.start_line = start;
		comment.start_side = 'RIGHT';
	}
	return comment;
}

type CommentCandidate = { suggested: ReviewComment; plain: ReviewComment };

export function nonconflictingComments(candidates: CommentCandidate[]): ReviewComment[] {
	return candidates.map((candidate, index) => {
		if (candidate.suggested.body === candidate.plain.body) return candidate.plain;
		const start = candidate.suggested.start_line ?? candidate.suggested.line;
		const end = candidate.suggested.line;
		const conflict = candidates.some((other, otherIndex) =>
			otherIndex !== index
			&& other.suggested.path === candidate.suggested.path && other.suggested.body !== other.plain.body
			&& (other.suggested.start_line ?? other.suggested.line) <= end && other.suggested.line >= start
		);
		return conflict ? candidate.plain : candidate.suggested;
	});
}

class ReviewAPI {
	private readonly base: string;
	private readonly token: string;
	private readonly request: typeof fetch;
	private readonly deadline = AbortSignal.timeout(60_000);
	constructor(base: string, token: string, request: typeof fetch) {
		this.base = base;
		this.token = token;
		this.request = request;
	}
	async call(path: string, body?: unknown): Promise<unknown> {
		const init: RequestInit = {
			method: body === undefined ? 'GET' : 'POST',
			headers: {
				Accept: 'application/vnd.github+json',
				Authorization: `Bearer ${this.token}`,
				'Content-Type': 'application/json',
				'X-GitHub-Api-Version': '2026-03-10',
			},
			redirect: 'error',
			signal: AbortSignal.any([this.deadline, AbortSignal.timeout(30_000)]),
		};
		if (body !== undefined) init.body = JSON.stringify(body);
		const response = await this.request(`${this.base}${path}`, init);
		if (!response.ok) throw new Error(`GitHub review API returned HTTP ${response.status}`);
		return response.json();
	}
	async list(path: string): Promise<unknown[]> {
		const values: unknown[] = [];
		for (let page = 1; page <= 30; page++) {
			const response = await this.call(`${path}?per_page=100&page=${page}`);
			if (!Array.isArray(response)) throw new Error('GitHub returned an invalid review listing');
			values.push(...response);
			if (response.length < 100) return values;
		}
		throw new Error('PR review exceeds the 3000-item listing limit');
	}
}

function workspacePath(environment: Environment, path: string): { absolute: string; relative: string } {
	const workspace = environment.GITHUB_WORKSPACE;
	if (!workspace) throw new Error('GITHUB_WORKSPACE is unavailable');
	const absolute = resolve(workspace, environment['INPUT_WORKING-DIRECTORY'] || '.', path);
	const local = relative(resolve(workspace), absolute);
	if (!local || local === '..' || local.startsWith(`..${sep}`) || isAbsolute(local)) {
		throw new Error('diagnostic path is outside the workspace');
	}
	return { absolute, relative: local.split(sep).join('/') };
}

async function sourceAtHead(
	api: ReviewAPI,
	ctx: ReviewContext,
	path: { absolute: string; relative: string },
	runtime: ReviewRuntime,
): Promise<string | undefined> {
	const url = `/repos/${ctx.sourceRepository}/contents/${
		path.relative.split('/').map(encodeURIComponent).join('/')
	}?ref=${ctx.sha}`;
	const response = await api.call(url);
	if (!object(response) || response.encoding !== 'base64' || typeof response.content !== 'string') return undefined;
	const expected = Buffer.from(response.content, 'base64').toString('utf8').replaceAll('\r\n', '\n');
	const actual = (await runtime.readSource(path.absolute)).replaceAll('\r\n', '\n');
	return actual === expected ? expected : undefined;
}

function normalizeFixPaths(diagnostic: Diagnostic, environment: Environment): Diagnostic {
	if (!diagnostic.fixes) return diagnostic;
	const fixes = diagnostic.fixes.map((fix) => ({
		description: fix.description,
		edits: fix.edits.map((edit): Edit => ({ ...edit, path: workspacePath(environment, edit.path).relative })),
	}));
	return { ...diagnostic, path: workspacePath(environment, diagnostic.path).relative, fixes };
}

export async function postReview(
	result: ActionResult,
	environment: Environment,
	supplied?: ReviewRuntime,
): Promise<void> {
	if (!result.completed || result.diagnostics.length === 0) return;
	const token = environment.INPUT_TOKEN?.trim();
	if (!token) throw new Error('review requires a token with pull-requests: write');
	if (!environment.GITHUB_EVENT_PATH) throw new Error('GITHUB_EVENT_PATH is unavailable');
	const ctx = context(JSON.parse(await readFile(environment.GITHUB_EVENT_PATH, 'utf8')), environment.GITHUB_REPOSITORY);
	const workspace = await realpath(environment.GITHUB_WORKSPACE || '.');
	const runtime: ReviewRuntime = supplied || {
		request: fetch,
		readSource: async (path) => {
			const canonical = await realpath(path);
			const local = relative(workspace, canonical);
			if (local === '..' || local.startsWith(`..${sep}`) || isAbsolute(local)) {
				throw new Error('source file resolves outside the workspace');
			}
			return readFile(canonical, 'utf8');
		},
	};
	const api = new ReviewAPI(
		(environment.GITHUB_API_URL || 'https://api.github.com').replace(/\/$/, ''),
		token,
		runtime.request,
	);
	const endpoint = `/repos/${ctx.repository}/pulls/${ctx.number}`;
	const current = await api.call(endpoint);
	if (!object(current) || !object(current.head) || current.head.sha !== ctx.sha) {
		throw new Error('the PR head changed after this analysis started');
	}
	const files = await api.list(`${endpoint}/files`);
	const previous = await api.list(`${endpoint}/comments`);
	const bodies = previous.flatMap((value) => object(value) && typeof value.body === 'string' ? [value.body] : []);
	const candidates: CommentCandidate[] = [];
	const sources = new Map<string, string | undefined>();
	for (const diagnostic of result.diagnostics) {
		const path = workspacePath(environment, diagnostic.path);
		const file = files.find((value) => object(value) && value.filename === path.relative);
		if (!object(file) || typeof file.patch !== 'string') continue;
		if (!sources.has(path.relative)) sources.set(path.relative, await sourceAtHead(api, ctx, path, runtime));
		const source = sources.get(path.relative);
		if (source === undefined) continue;
		const normalized = normalizeFixPaths(diagnostic, environment);
		const hunks = diffHunks(file.patch);
		const suggested = reviewComment(normalized, path.relative, source, hunks, ctx.sha);
		const plain = reviewComment(normalized, path.relative, source, hunks, ctx.sha, false);
		if (suggested && plain) candidates.push({ suggested, plain });
	}
	const comments: ReviewComment[] = [];
	for (const comment of nonconflictingComments(candidates)) {
		const marker = comment.body.slice(comment.body.lastIndexOf('<!-- actionlint:'));
		if (bodies.some((body) => body.includes(marker))) continue;
		comments.push(comment);
		bodies.push(comment.body);
		if (comments.length === 50) break;
	}
	if (comments.length === 0) return;
	const latest = await api.call(endpoint);
	if (!object(latest) || !object(latest.head) || latest.head.sha !== ctx.sha) {
		throw new Error('the PR head changed while preparing this review');
	}
	await api.call(`${endpoint}/reviews`, {
		commit_id: ctx.sha,
		event: 'COMMENT',
		body:
			`actionlint found ${result.diagnostics.length} problems. This review contains ${comments.length} new comments on changed lines; the complete results remain in the workflow outputs.`,
		comments,
	});
}
