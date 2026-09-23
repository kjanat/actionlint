import assert from 'node:assert/strict';
import { test } from 'node:test';

import { once } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer, type Server } from 'node:http';
import { connect, type Socket } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { ActionResult, Diagnostic, Fix } from '#result';
import { object } from '#result';
import type { ReviewRuntime } from '#review';
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
		files?: string[];
	} = {},
) {
	const calls: string[] = [];
	const writes: unknown[] = [];
	const files = options.files ?? ['ci.yml'];
	let headReads = 0;
	const request: ReviewRuntime['request'] = async (input, init) => {
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
			const entries = [
				...(options.paginated
					? Array.from({ length: 100 }, (_, index) => ({ filename: `unrelated-${index}.yml` }))
					: []),
				...files.map((filename) => ({ filename, patch })),
			];
			const page = Number(new URL(url).searchParams.get('page'));
			data = entries.slice((page - 1) * 100, page * 100);
		} else if (url.includes('/comments?')) {
			data = (options.existing || []).map((body) => ({ body }));
		} else if (url.includes('/contents/')) {
			assert.ok(files.some((path) => url.includes(`/repos/fork/repo/contents/${path}?ref=${sha}`)));
			data = { encoding: 'base64', content: Buffer.from(options.source ?? source).toString('base64') };
		} else {
			assert.ok(url.endsWith('/pulls/5'));
			data = { head: { sha: headReads++ === 0 ? options.head || sha : options.changedHead || sha } };
		}
		return Response.json(data);
	};
	const readSource: ReviewRuntime['readSource'] = async () => source;
	return { calls, writes, request, readSource };
}

async function listen(server: Server): Promise<number> {
	server.listen(0, '127.0.0.1');
	await once(server, 'listening');
	const address = server.address();
	assert.ok(address && typeof address === 'object');
	return address.port;
}

test('default review API uses the runner proxy and honors NO_PROXY', { timeout: 5_000 }, async (t) => {
	const variables = ['HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY', 'http_proxy', 'https_proxy', 'no_proxy'];
	const original = new Map(variables.map((name) => [name, process.env[name]]));
	const sockets = new Set<Socket>();
	const requests: string[] = [];
	const tunnels: string[] = [];
	let status = 200;
	const origin = createServer((request, response) => {
		requests.push(request.url ?? '');
		response.writeHead(status, { 'Content-Type': 'application/json' });
		response.end(JSON.stringify(request.url?.endsWith('/pulls/5') ? { head: { sha } } : []));
	});
	const proxy = createServer();
	for (const server of [origin, proxy]) {
		server.on('connection', (socket) => {
			sockets.add(socket);
			socket.once('close', () => sockets.delete(socket));
		});
	}
	t.after(async () => {
		for (const [name, value] of original) {
			if (value === undefined) delete process.env[name];
			else process.env[name] = value;
		}
		for (const socket of sockets) socket.destroy();
		await Promise.all([origin, proxy].map((server) =>
			new Promise<void>((resolve, reject) => {
				if (!server.listening) return resolve();
				server.close((error) => error ? reject(error) : resolve());
			})
		));
	});
	const originPort = await listen(origin);
	proxy.on('connect', (request, socket, head) => {
		tunnels.push(request.url ?? '');
		const upstream = connect(originPort, '127.0.0.1');
		sockets.add(upstream);
		upstream.once('close', () => sockets.delete(upstream));
		upstream.once('error', () => socket.destroy());
		socket.once('error', () => upstream.destroy());
		upstream.once('connect', () => {
			socket.write('HTTP/1.1 200 Connection Established\r\n\r\n');
			if (head.length) upstream.write(head);
			socket.pipe(upstream).pipe(socket);
		});
	});
	const proxyPort = await listen(proxy);
	for (const name of variables) delete process.env[name];
	process.env.HTTP_PROXY = `http://127.0.0.1:${proxyPort}`;
	await fixture(async (environment) => {
		const proxied = { ...environment, GITHUB_API_URL: 'http://proxy-only.invalid' };
		await postReview(result, proxied);
		assert.deepEqual(requests, [
			'/repos/owner/repo/pulls/5',
			'/repos/owner/repo/pulls/5/files?per_page=100&page=1',
			'/repos/owner/repo/pulls/5/comments?per_page=100&page=1',
		]);
		assert.ok(tunnels.length > 0);
		assert.ok(tunnels.every((target) => target === 'proxy-only.invalid:80'));
		status = 403;
		await assert.rejects(postReview(result, proxied), /GitHub review API returned HTTP 403/);
		status = 200;
		const beforeBypass = tunnels.length;
		process.env.NO_PROXY = '127.0.0.1';
		await postReview(result, { ...environment, GITHUB_API_URL: `http://127.0.0.1:${originPort}` });
		assert.equal(tunnels.length, beforeBypass);
	});
});

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

function diagnosticAt(path: string): Diagnostic {
	return {
		...diagnostic,
		path,
		fixes: (diagnostic.fixes ?? []).map((fix) => ({
			...fix,
			edits: fix.edits.map((edit) => ({ ...edit, path })),
		})),
	};
}

test('review stops source requests after 50 new eligible comments', async () => {
	await fixture(async (environment) => {
		const diagnostics = Array.from({ length: 75 }, (_, index) => ({ ...diagnosticAt(`ci-${index}.yml`), fixes: [] }));
		const contextOnly: Diagnostic = {
			...diagnosticAt('context.yml'),
			start: { line: 2, column: 1 },
			end: { line: 2, column: 5 },
			fixes: [],
		};
		const existing = diagnostics.slice(0, 5).map((value) => {
			const comment = reviewComment(value, value.path, source, diffHunks(patch), sha);
			assert.ok(comment);
			return comment.body;
		});
		const runtime = api({ files: [contextOnly.path, ...diagnostics.map((value) => value.path)], existing });
		await postReview(
			{ ...result, diagnostics: [contextOnly, ...diagnostics.flatMap((value) => [value, value])] },
			environment,
			runtime,
		);
		const review = runtime.writes[0];
		assert.ok(object(review) && Array.isArray(review.comments));
		assert.deepEqual(
			review.comments.map((comment) => {
				assert.ok(object(comment));
				return comment.path;
			}),
			diagnostics.slice(5, 55).map((value) => value.path),
		);
		assert.equal(runtime.calls.filter((url) => url.includes('/contents/')).length, 55);
	});
});

test('review skips source reads for unchanged context but retains fixes on changed lines', async () => {
	await fixture(async (environment) => {
		const contextOnly = Array.from({ length: 75 }, (_, index): Diagnostic => ({
			...diagnosticAt(`context-${index}.yml`),
			start: { line: 2, column: 1 },
			end: { line: 2, column: 5 },
			fixes: [],
		}));
		const changedFix: Diagnostic = {
			...diagnosticAt('changed-fix.yml'),
			start: { line: 2, column: 1 },
			end: { line: 2, column: 5 },
		};
		const diagnostics = [...contextOnly, changedFix, diagnostic];
		const runtime = api({ files: diagnostics.map((value) => value.path) });
		await postReview({ ...result, diagnostics }, environment, runtime);
		assert.deepEqual(
			runtime.calls.filter((url) => url.includes('/contents/')),
			['changed-fix.yml', 'ci.yml'].map((path) => `https://api.github.com/repos/fork/repo/contents/${path}?ref=${sha}`),
		);
		const review = runtime.writes[0];
		assert.ok(object(review) && Array.isArray(review.comments));
		assert.equal(review.comments.length, 2);
		for (const comment of review.comments) {
			assert.ok(object(comment) && typeof comment.body === 'string');
			assert.ok(comment.body.includes('```suggestion'));
			assert.equal(comment.line, 3);
		}
	});
});

test('review bounds source lookups even when remaining files do not produce comments', async () => {
	await fixture(async (environment) => {
		const diagnostics = Array.from({ length: 125 }, (_, index) => diagnosticAt(`ci-${index}.yml`));
		const runtime = api({ files: diagnostics.map((value) => value.path) });
		runtime.readSource = async (path: string) => path.endsWith('ci-0.yml') ? source : `${source}modified\n`;
		await postReview({ ...result, diagnostics }, environment, runtime);
		assert.equal(runtime.calls.filter((url) => url.includes('/contents/')).length, 100);
		const review = runtime.writes[0];
		assert.ok(object(review) && Array.isArray(review.comments));
		assert.equal(review.comments.length, 1);
		assert.ok(object(review.comments[0]));
		assert.equal(review.comments[0].path, 'ci-0.yml');
	});
});

test('review accounts for late overlapping suggestions before the comment limit', async () => {
	await fixture(async (environment) => {
		const first = diagnosticAt('first.yml');
		const later = { ...first, code: 'SC9999', message: 'Another fix for the same line' };
		const diagnostics = Array.from({ length: 60 }, (_, index) => diagnosticAt(`ci-${index}.yml`));
		const existing = reviewComment(first, first.path, source, diffHunks(patch), sha, false);
		assert.ok(existing);
		const runtime = api({ files: [first.path, ...diagnostics.map((value) => value.path)], existing: [existing.body] });
		await postReview({ ...result, diagnostics: [first, ...diagnostics, later] }, environment, runtime);
		const review = runtime.writes[0];
		assert.ok(object(review) && Array.isArray(review.comments));
		assert.deepEqual(
			review.comments.map((comment) => {
				assert.ok(object(comment));
				return comment.path;
			}),
			diagnostics.slice(0, 50).map((value) => value.path),
		);
		assert.equal(runtime.calls.filter((url) => url.includes('/contents/')).length, 51);
	});
});
