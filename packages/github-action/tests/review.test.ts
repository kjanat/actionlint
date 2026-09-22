import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { ActionResult, Diagnostic, Fix } from '#result';
import { object } from '#result';
import { diffHunks, postReview, reviewComment, suggestion } from '#review';
import type { Environment } from '#runtime';

const sha = 'a'.repeat(40);
const diagnostic: Diagnostic = {
	rule: 'shellcheck',
	code: 'SC2086',
	message: 'Quote the variable',
	path: 'ci.yml',
	start: { line: 3, column: 8 },
	end: { line: 3, column: 10 },
	fixes: [{
		description: 'Quote',
		edits: [
			{ path: 'ci.yml', start: { line: 3, column: 8 }, end: { line: 3, column: 8 }, replacement: '"' },
			{ path: 'ci.yml', start: { line: 3, column: 10 }, end: { line: 3, column: 10 }, replacement: '"' },
		],
	}],
};
const source = 'jobs:\n  test:\n  echo $x\n';
const patch = '@@ -1,3 +1,3 @@\n jobs:\n   test:\n-  echo ok\n+  echo $x\n';
const result: ActionResult = {
	schema_version: 1,
	completed: true,
	status: 'problems-found',
	exit_code: 1,
	file_count: 1,
	diagnostics: [diagnostic],
	configurations: [],
	hints: [],
};

test('whole fix groups produce line suggestions using Unicode columns', () => {
	const fix = diagnostic.fixes?.[0];
	assert.ok(fix);
	assert.deepEqual(suggestion(fix, 'ci.yml', source), { start: 3, end: 3, replacement: '  echo "$x"' });
	const unicode: Fix = {
		description: 'replace',
		edits: [{
			path: 'ci.yml',
			start: { line: 1, column: 3 },
			end: { line: 1, column: 5 },
			replacement: 'ok',
		}],
	};
	assert.deepEqual(suggestion(unicode, 'ci.yml', '🦕 $x\n'), { start: 1, end: 1, replacement: '🦕 ok' });
	assert.equal(suggestion({ ...fix, edits: [...fix.edits, ...fix.edits] }, 'ci.yml', source), undefined);
	assert.equal(
		suggestion({ ...fix, edits: fix.edits.map((edit) => ({ ...edit, path: 'other.yml' })) }, 'ci.yml', source),
		undefined,
	);
});

test('reviews use new-side changed lines and stable markers', () => {
	const hunks = diffHunks(patch);
	const comment = reviewComment(diagnostic, 'ci.yml', source, hunks, sha);
	assert.ok(comment);
	assert.equal(comment.line, 3);
	assert.equal(comment.side, 'RIGHT');
	assert.ok(comment.body.includes('```suggestion\n  echo "$x"\n```'));
	assert.equal(reviewComment(diagnostic, 'ci.yml', source, hunks, sha)?.body, comment.body);
	const contextOnly = { ...diagnostic, start: { line: 2, column: 1 }, end: { line: 2, column: 5 }, fixes: [] };
	assert.equal(reviewComment(contextOnly, 'ci.yml', source, hunks, sha), undefined);
	assert.equal(reviewComment(diagnostic, 'ci.yml', source, [], sha), undefined);
});

async function fixture(run: (environment: Environment) => Promise<void>): Promise<void> {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-review-test-'));
	try {
		const event = join(directory, 'event.json');
		await writeFile(
			event,
			JSON.stringify({ number: 5, pull_request: { head: { sha, repo: { full_name: 'fork/repo' } } } }),
		);
		await run({
			GITHUB_EVENT_PATH: event,
			GITHUB_WORKSPACE: directory,
			GITHUB_REPOSITORY: 'owner/repo',
			INPUT_TOKEN: 'fixture-token',
		});
	} finally {
		await rm(directory, { recursive: true });
	}
}

function api(
	options: {
		status?: number;
		existing?: string[];
		head?: string;
		changedHead?: string;
		source?: string;
		paginated?: boolean;
	} = {},
) {
	const calls: string[] = [];
	const writes: unknown[] = [];
	let headReads = 0;
	const request: typeof fetch = async (input, init) => {
		const url = String(input);
		calls.push(url);
		if (options.status) return new Response('', { status: options.status });
		let data: unknown;
		if (url.includes('/reviews')) {
			assert.equal(init?.method, 'POST');
			assert.equal(typeof init.body, 'string');
			if (typeof init.body === 'string') writes.push(JSON.parse(init.body));
			data = { id: 1 };
		} else if (url.includes('/files?')) {
			data = options.paginated && url.endsWith('page=1')
				? Array.from({ length: 100 }, (_, index) => ({ filename: `unrelated-${index}.yml` }))
				: [{ filename: 'ci.yml', patch }];
		} else if (url.includes('/comments?')) {
			data = (options.existing || []).map((body) => ({ body }));
		} else if (url.includes('/contents/')) {
			assert.ok(url.includes(`/repos/fork/repo/contents/ci.yml?ref=${sha}`));
			data = { encoding: 'base64', content: Buffer.from(options.source ?? source).toString('base64') };
		} else {
			assert.ok(url.endsWith('/pulls/5'));
			data = { head: { sha: headReads++ === 0 ? options.head || sha : options.changedHead || sha } };
		}
		return Response.json(data);
	};
	return { calls, writes, request, readSource: async () => source };
}

test('review API targets event head and groups suggestions after paginated diff inspection', async () => {
	await fixture(async (environment) => {
		const runtime = api({ paginated: true });
		await postReview(result, environment, runtime);
		assert.equal(runtime.writes.length, 1);
		const review = runtime.writes[0];
		assert.ok(object(review));
		assert.equal(review.commit_id, sha);
		assert.equal(review.event, 'COMMENT');
		assert.ok(Array.isArray(review.comments));
		assert.equal(review.comments.length, 1);
		assert.ok(runtime.calls.some((url) => url.endsWith('/files?per_page=100&page=2')));
	});
});

test('reruns deduplicate existing review comments without publishing again', async () => {
	await fixture(async (environment) => {
		const existing = reviewComment(diagnostic, 'ci.yml', source, diffHunks(patch), sha);
		assert.ok(existing);
		const runtime = api({ existing: [existing.body] });
		await postReview(result, environment, runtime);
		assert.equal(runtime.writes.length, 0);
	});
});

test('read-only fork tokens and stale event heads cannot post', async () => {
	await fixture(async (environment) => {
		for (const options of [{ status: 403 }, { head: 'b'.repeat(40) }, { changedHead: 'c'.repeat(40) }]) {
			const runtime = api(options);
			await assert.rejects(postReview(result, environment, runtime), /HTTP 403|head changed/);
			assert.equal(runtime.writes.length, 0);
		}
	});
});

test('source modified after checkout cannot generate suggestions for the PR head', async () => {
	await fixture(async (environment) => {
		const runtime = api({ source: source.replace('$x', '$different') });
		await postReview(result, environment, runtime);
		assert.equal(runtime.writes.length, 0);
	});
});

test('incomplete analysis never initiates a review request', async () => {
	const runtime = api();
	await postReview({ ...result, completed: false, status: 'failure', exit_code: 3, error: 'incomplete' }, {}, runtime);
	assert.equal(runtime.calls.length, 0);
});

test('overlapping suggestions from separate diagnostics remain explanatory comments', async () => {
	await fixture(async (environment) => {
		const runtime = api();
		const overlapping: Diagnostic = { ...diagnostic, code: 'SC9999', message: 'Another fix for the same line' };
		await postReview({ ...result, diagnostics: [diagnostic, overlapping] }, environment, runtime);
		const review = runtime.writes[0];
		assert.ok(object(review) && Array.isArray(review.comments));
		assert.equal(review.comments.length, 2);
		for (const comment of review.comments) {
			assert.ok(object(comment) && typeof comment.body === 'string');
			assert.ok(!comment.body.includes('```suggestion'));
			assert.equal(comment.line, 3);
		}
	});
});
