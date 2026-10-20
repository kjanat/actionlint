import assert from 'node:assert/strict';
import test from 'node:test';

import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, dirname, join, resolve } from 'node:path';

import { collectAssets, publishWinget, publishWingetAndReport, reportWingetFailure } from './release-assets.mjs';

/** @param {import('node:test').TestContext} t */
async function fixture(t) {
	const tempRoot = resolve(tmpdir());
	const root = await mkdtemp(join(tempRoot, 'actionlint-release-assets-'));
	t.after(async () => {
		if (dirname(root) === tempRoot) await rm(root, { recursive: true, force: true });
	});
	return root;
}

test('collect preserves release bytes and generated package manifests without uploading build metadata', async (t) => {
	const root = await fixture(t);
	const output = join(root, 'assets');
	await mkdir(output);
	const files = [
		'homebrew/Casks/actionlint.rb',
		'scoop/bucket/actionlint.json',
		'aur/actionlint-kjanat-bin.pkgbuild',
		'aur/actionlint-kjanat-bin.srcinfo',
		'aur/actionlint-kjanat.pkgbuild',
		'aur/actionlint-kjanat.srcinfo',
		'actionlint_1.2.3_linux_amd64.tar.gz',
		'actionlint_1.2.3_checksums.txt',
		'actionlint.sbom.json',
		'linux_amd64_v1/actionlint',
	];
	for (const file of files) {
		const path = join(root, 'dist', file);
		await mkdir(dirname(path), { recursive: true });
		await writeFile(path, `${file}\r\nbytes\0`);
	}
	await writeFile(join(output, 'actionlint-action_1.2.3.mjs'), 'bundle');
	await writeFile(
		join(root, 'dist/artifacts.json'),
		JSON.stringify([
			{ type: 'Archive', name: files[6], path: `dist/${files[6]}` },
			{ type: 'Checksum', name: files[7], path: `dist/${files[7]}` },
			{ type: 'SBOM', name: files[8], path: `dist/${files[8]}` },
			{ type: 'Binary', internal_type: 4, name: 'actionlint', path: `dist/${files[9]}` },
			{ type: 'File', name: 'actionlint-action_1.2.3.mjs', path: join(output, 'actionlint-action_1.2.3.mjs') },
		]),
	);
	await collectAssets(root, output);
	assert.equal((await readdir(output)).length, 10);
	assert.deepEqual(await readFile(join(output, files[6])), await readFile(join(root, 'dist', files[6])));
	assert.deepEqual(
		await readFile(join(output, 'actionlint-homebrew.rb')),
		await readFile(join(root, 'dist', files[0])),
	);
	assert.deepEqual(
		await readFile(join(output, 'actionlint-kjanat.srcinfo')),
		await readFile(join(root, 'dist', files[5])),
	);
	assert.ok(!(await readdir(output)).includes('artifacts.json'));
	for (const file of files.slice(10)) {
		assert.deepEqual(await readFile(join(output, basename(file))), await readFile(join(root, 'dist', file)));
	}
});

test('collection rejects absent outputs and name collisions rather than dropping artifacts', async (t) => {
	const root = await fixture(t);
	await mkdir(join(root, 'dist'));
	await writeFile(join(root, 'dist/artifacts.json'), '[]');
	await assert.rejects(collectAssets(root, join(root, 'assets')), /no release archives/);
	await writeFile(join(root, 'dist/a.zip'), 'archive');
	await writeFile(
		join(root, 'dist/artifacts.json'),
		JSON.stringify([
			{ type: 'Archive', name: 'a.zip', path: 'dist/a.zip' },
			{ type: 'Archive', name: 'a.zip', path: 'dist/a.zip' },
		]),
	);
	await assert.rejects(collectAssets(root, join(root, 'assets')), /EEXIST/);
});

/** @param {import('node:test').TestContext} t @param {string} [version] */
async function wingetFixture(t, version = '1.2.3') {
	const root = await fixture(t);
	const names = ['kjanat.actionlint.yaml', 'kjanat.actionlint.installer.yaml', 'kjanat.actionlint.locale.en-US.yaml'];
	const files = names.map(name => ({
		path: `manifests/k/kjanat/actionlint/${version}/${name}`,
		mode: '100644',
		type: 'blob',
		content: `\uFEFF# ${name}\r\nPackageVersion: ${version}\r\nPublisher: Kaj Kowalski\r\n`,
	}));
	for (const file of files) await writeFile(join(root, basename(file.path)), file.content);
	/** @type {{
		refs: Map<string, string>, published: Map<string, string>,
		pulls: {number: number, url: string, state: string, paths: string[], head: string}[],
		issues: {number: number, body: string, state: string, pull_request?: object}[],
		calls: {endpoint: string, method: string, body: any}[],
		scans: number, onScan: (number: number) => void | Promise<void>, onCreate: () => void,
		failAt: string, ambiguousCreation: boolean, incomplete: boolean, graphQLError: boolean,
		lastTree: typeof files
	}} */
	const state = {
		refs: new Map(),
		published: new Map(),
		pulls: [],
		issues: [],
		calls: [],
		scans: 0,
		onScan: () => {},
		onCreate: () => {},
		failAt: '',
		ambiguousCreation: false,
		incomplete: false,
		graphQLError: false,
		lastTree: files,
	};
	const parentSHA = '1'.repeat(40), baseTree = '2'.repeat(40), nextTree = '3'.repeat(40), nextCommit = '4'.repeat(40);
	/** @param {any[]} nodes @param {number} limit @param {string | null} cursor */
	const connection = (nodes, limit, cursor) => {
		const start = cursor ? Number(cursor) : 0;
		const end = Math.min(start + limit, nodes.length);
		return {
			totalCount: nodes.length,
			nodes: nodes.slice(start, end),
			pageInfo: { hasNextPage: end < nodes.length, endCursor: String(end) },
		};
	};
	/** @type {typeof fetch} */
	const request = async (url, options) => {
		assert.ok(options);
		const endpoint = String(url).replace('https://api.github.com/', '');
		const method = options.method ?? 'GET';
		const body = typeof options.body === 'string' ? JSON.parse(options.body) : undefined;
		state.calls.push({ endpoint, method, body });
		const ownIssues = endpoint.startsWith('repos/kjanat/actionlint/issues');
		assert.equal(
			new Headers(options.headers).get('Authorization'),
			ownIssues ? 'Bearer test-issues' : 'Bearer test-winget',
		);
		assert.equal(options.redirect, 'error');
		if (endpoint === state.failAt) return new Response('', { status: 503 });
		if (endpoint.includes('/contents/')) {
			const path = endpoint.slice('repos/microsoft/winget-pkgs/contents/'.length).split('?')[0];
			const file = files.find(file => basename(path) === basename(file.path));
			assert.ok(file);
			const content = state.published.get(path);
			return content === undefined
				? new Response('', { status: 404 })
				: Response.json({ encoding: 'base64', content: Buffer.from(content).toString('base64') });
		}
		if (endpoint.startsWith('repos/microsoft/winget-pkgs/pulls?')) {
			return Response.json(
				state.pulls.filter(pull => pull.head === new URL(String(url)).searchParams.get('head'))
					.map(pull => ({ state: pull.state, html_url: pull.url })),
			);
		}
		if (endpoint === 'repos/microsoft/winget-pkgs/git/ref/heads/master') {
			return Response.json({ object: { sha: parentSHA } });
		}
		if (endpoint.startsWith('repos/kjanat/winget-pkgs/git/ref/')) {
			const name = endpoint.slice('repos/kjanat/winget-pkgs/git/ref/'.length);
			const sha = state.refs.get(`refs/${name}`);
			return sha ? Response.json({ object: { sha } }) : new Response('', { status: 404 });
		}
		if (endpoint === 'repos/kjanat/winget-pkgs/git/refs') {
			assert.equal(method, 'POST');
			if (state.refs.has(body.ref)) return new Response('', { status: 422 });
			state.refs.set(body.ref, body.sha);
			return Response.json({ ref: body.ref, object: { sha: body.sha } });
		}
		if (endpoint === 'repos/kjanat/winget-pkgs/git/refs/heads/actionlint-winget-submission-lock') {
			assert.equal(method, 'DELETE');
			assert.ok(state.refs.delete('refs/heads/actionlint-winget-submission-lock'));
			return new Response(null, { status: 204 });
		}
		if (endpoint === 'graphql') {
			assert.equal(method, 'POST');
			if (state.graphQLError) {
				return Response.json({ errors: [{ message: 'Cannot read files' }], data: { repository: {} } });
			}
			if (body.query.includes('pullRequests(')) {
				if (!body.variables.cursor) await state.onScan(++state.scans);
				const open = state.pulls.filter(pull => pull.state === 'open').map(pull => ({
					number: pull.number,
					url: pull.url,
					files: connection(pull.paths.map(path => ({ path })), 100, null),
				}));
				const pulls = connection(open, 50, body.variables.cursor);
				if (state.incomplete) pulls.totalCount++;
				return Response.json({ data: { repository: { pullRequests: pulls } } });
			}
			const pull = state.pulls.find(pull => pull.number === body.variables.number);
			assert.ok(pull);
			return Response.json({
				data: {
					repository: {
						pullRequest: {
							files: connection(pull.paths.map(path => ({ path })), 100, body.variables.cursor),
						},
					},
				},
			});
		}
		if (endpoint === `repos/kjanat/winget-pkgs/git/commits/${parentSHA}`) {
			return Response.json({ tree: { sha: baseTree } });
		}
		if (endpoint === 'repos/kjanat/winget-pkgs/git/trees') {
			assert.deepEqual(body, { base_tree: baseTree, tree: files });
			state.lastTree = body.tree;
			return Response.json({ sha: nextTree });
		}
		if (endpoint === 'repos/kjanat/winget-pkgs/git/commits') {
			assert.equal(body.tree, nextTree);
			assert.deepEqual(body.parents, [parentSHA]);
			return Response.json({ sha: nextCommit });
		}
		if (endpoint === 'repos/microsoft/winget-pkgs/pulls') {
			assert.equal(method, 'POST');
			assert.equal(body.head, `kjanat:actionlint-${version}`);
			assert.equal(body.base, 'master');
			assert.equal(body.draft, false);
			state.pulls.push({
				number: 123,
				url: 'https://github.com/microsoft/winget-pkgs/pull/123',
				state: 'open',
				paths: state.lastTree.map(file => file.path),
				head: body.head,
			});
			state.onCreate();
			if (state.ambiguousCreation) throw new Error('Connection lost after GitHub accepted the submission');
			return Response.json({ number: 123, html_url: 'https://github.com/microsoft/winget-pkgs/pull/123' });
		}
		if (endpoint === 'repos/microsoft/winget-pkgs/pulls/123') {
			assert.equal(method, 'PATCH');
			assert.deepEqual(body, { state: 'closed' });
			const pull = state.pulls.find(pull => pull.number === 123);
			assert.ok(pull);
			pull.state = 'closed';
			return Response.json({ state: 'closed' });
		}
		if (endpoint.startsWith('repos/kjanat/actionlint/issues?')) {
			const page = Number(new URL(String(url)).searchParams.get('page'));
			return Response.json(state.issues.slice((page - 1) * 100, page * 100));
		}
		if (endpoint === 'repos/kjanat/actionlint/issues') {
			assert.equal(method, 'POST');
			const number = state.issues.length + 1;
			state.issues.push({ number, body: body.body, state: 'open' });
			return Response.json({ number });
		}
		if (endpoint.startsWith('repos/kjanat/actionlint/issues/')) {
			assert.equal(method, 'PATCH');
			const issue = state.issues.find(issue => issue.number === Number(endpoint.split('/').at(-1)));
			assert.ok(issue);
			Object.assign(issue, body);
			return Response.json(issue);
		}
		assert.fail(`Unexpected request: ${method} ${endpoint}`);
	};
	/** @type {NodeJS.ProcessEnv} */
	const env = { WINGET_TOKEN: 'test-winget', GITHUB_TOKEN: 'test-issues', GITHUB_RUN_ID: '42' };
	const publish = () => publishWinget(root, version, env, request);
	const submit = () => publishWingetAndReport(root, version, env, request);
	const creationCalls = () =>
		state.calls.filter(call => call.method === 'POST' && call.endpoint === 'repos/microsoft/winget-pkgs/pulls');
	/** @param {number} number @param {string[]} paths @param {string} [head] */
	const pull = (number, paths, head = 'someone:any-branch') => ({
		number,
		url: `https://github.com/microsoft/winget-pkgs/pull/${number}`,
		state: 'open',
		paths,
		head,
	});
	return { root, files, state, request, env, publish, submit, creationCalls, pull };
}

test('WinGet submits verified bytes once and preserves the permanent attempt marker', async t => {
	const f = await wingetFixture(t);
	await f.submit();
	assert.equal(f.creationCalls().length, 1);
	const submission = f.creationCalls()[0];
	assert.ok(submission);
	assert.match(submission.body.body, /\/blob\/master\/distribution\/winget\/README\.md/);
	assert.doesNotMatch(submission.body.body, /\/blob\/v1\.2\.3\/distribution\/winget\//);
	assert.ok(f.state.refs.has('refs/tags/actionlint-winget-attempt-1.2.3'));
	assert.ok(!f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
	await assert.rejects(f.submit(), /already attempted/);
	assert.equal(f.creationCalls().length, 1);
	assert.equal(f.state.issues.length, 1);
	await assert.rejects(f.submit(), /already attempted/);
	assert.equal(f.state.issues.length, 1);
});

test('WinGet blocks other contributors and other versions using changed files, even on later pages', async t => {
	for (
		const scenario of ['other author', 'later PR page', 'later file page', 'own earlier version', 'case variation']
	) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			if (scenario === 'later PR page') {
				for (let i = 0; i < 50; i++) f.state.pulls.push(f.pull(1000 + i, ['manifests/a/Another/Package/file.yaml']));
			}
			const paths = scenario === 'later file page' ? Array.from({ length: 100 }, (_, i) => `unrelated/${i}`) : [];
			paths.push(
				scenario === 'case variation'
					? 'manifests/k/KJANAT/ActionLint/9.9.9/file.yaml'
					: 'manifests/k/kjanat/actionlint/9.9.9/file.yaml',
			);
			f.state.pulls.push(f.pull(99, paths, scenario === 'own earlier version' ? 'kjanat:actionlint-9.9.9' : undefined));
			await assert.rejects(f.submit(), /blocked.*pull\/99/);
			assert.equal(f.creationCalls().length, 0);
			assert.equal(f.state.issues.length, 1);
			assert.match(f.state.issues[0].body, /pull\/99/);
			assert.match(f.state.issues[0].body, /actions\/runs\/42/);
			assert.ok(!f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
			assert.ok(!f.state.refs.has('refs/tags/actionlint-winget-attempt-1.2.3'));
		});
	}
});

test('WinGet rejects prior branches, deleted-branch attempts and closed submissions', async t => {
	for (const scenario of ['branch', 'attempt', 'closed PR']) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			if (scenario === 'branch') f.state.refs.set('refs/heads/actionlint-1.2.3', '1'.repeat(40));
			if (scenario === 'attempt') f.state.refs.set('refs/tags/actionlint-winget-attempt-1.2.3', '1'.repeat(40));
			if (scenario === 'closed PR') {
				f.state.pulls.push({ ...f.pull(99, [], 'kjanat:actionlint-1.2.3'), state: 'closed' });
			}
			await assert.rejects(f.submit(), /already/);
			assert.equal(f.creationCalls().length, 0);
			assert.equal(f.state.issues.length, 1);
		});
	}
});

test('WinGet skips identical published files and reports conflicting or partial publication', async t => {
	for (const scenario of ['identical', 'conflicting', 'partial']) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			for (const file of f.files) {
				if (scenario === 'partial' && file !== f.files[0]) continue;
				f.state.published.set(file.path, scenario === 'conflicting' ? 'different' : file.content);
			}
			if (scenario === 'identical') await f.submit();
			else await assert.rejects(f.submit(), /different contents|missing some package files/);
			assert.equal(f.creationCalls().length, 0);
			assert.equal(f.state.issues.length, scenario === 'identical' ? 0 : 1);
			assert.equal(f.state.refs.size, 0);
		});
	}
});

test('WinGet does not submit a version merged by another contributor during preparation', async t => {
	const f = await wingetFixture(t);
	f.state.onScan = scan => {
		if (scan === 2) { for (const file of f.files) f.state.published.set(file.path, file.content); }
	};
	await assert.rejects(f.submit(), /published during preparation/);
	assert.equal(f.creationCalls().length, 0);
	assert.equal(f.state.issues.length, 1);
	assert.ok(!f.state.refs.has('refs/tags/actionlint-winget-attempt-1.2.3'));
	assert.ok(!f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
});

test('WinGet shared lock prevents overlapping publishers and is not deleted by a losing publisher', async t => {
	const f = await wingetFixture(t);
	f.state.refs.set('refs/heads/actionlint-winget-submission-lock', '9'.repeat(40));
	await assert.rejects(f.submit(), /HTTP 422/);
	assert.equal(f.creationCalls().length, 0);
	assert.equal(f.state.refs.get('refs/heads/actionlint-winget-submission-lock'), '9'.repeat(40));
	assert.equal(f.state.issues.length, 1);
});

test('WinGet serializes simultaneous publishers, including different versions', async t => {
	for (const version of ['1.2.3', '1.2.4']) {
		await t.test(version, async t => {
			const f = await wingetFixture(t);
			const second = await wingetFixture(t, version);
			const entered = Promise.withResolvers();
			const resume = Promise.withResolvers();
			f.state.onScan = async scan => {
				if (scan === 1) {
					entered.resolve(undefined);
					await resume.promise;
				}
			};
			const first = f.submit();
			try {
				await entered.promise;
				await assert.rejects(
					publishWingetAndReport(second.root, version, second.env, f.request),
					/shared WinGet submission lock/,
				);
				assert.ok(f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
				assert.equal(f.creationCalls().length, 0);
				assert.equal(f.state.issues.length, 1);
			} finally {
				resume.resolve(undefined);
				await first;
			}
			assert.equal(f.creationCalls().length, 1);
			assert.equal(f.state.pulls.filter(p => p.state === 'open').length, 1);
			assert.equal(f.state.calls.filter(c => c.method === 'DELETE').length, 1);
		});
	}
});

test('WinGet rechecks before creation and withdraws its PR if an independent submission races creation', async t => {
	for (const scenario of ['before creation', 'after creation', 'postflight API failure']) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			if (scenario === 'before creation') {
				f.state.onScan = scan => {
					if (scan === 2) f.state.pulls.push(f.pull(99, [f.files[0].path]));
				};
			}
			if (scenario === 'after creation') f.state.onCreate = () => f.state.pulls.push(f.pull(99, [f.files[0].path]));
			if (scenario === 'postflight API failure') {
				f.state.onCreate = () => {
					f.state.graphQLError = true;
				};
			}
			await assert.rejects(f.submit(), /blocked|Concurrent|GraphQL/);
			assert.equal(f.creationCalls().length, scenario === 'before creation' ? 0 : 1);
			if (scenario !== 'before creation') assert.equal(f.state.pulls.find(p => p.number === 123)?.state, 'closed');
			assert.equal(f.state.issues.length, 1);
			assert.ok(!f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
		});
	}
});

test('WinGet never retries PR creation after an ambiguous timeout, even if the branch is deleted', async t => {
	const f = await wingetFixture(t);
	f.state.ambiguousCreation = true;
	await assert.rejects(f.submit(), /Connection lost/);
	assert.equal(f.creationCalls().length, 1);
	assert.ok(f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
	f.state.refs.delete('refs/heads/actionlint-1.2.3');
	await assert.rejects(f.submit(), /already attempted/);
	assert.equal(f.creationCalls().length, 1);
	assert.equal(f.state.issues.length, 1);
});

test('WinGet retains its lock and reports both errors when PR withdrawal fails', async t => {
	const f = await wingetFixture(t);
	f.state.onCreate = () => f.state.pulls.push(f.pull(99, [f.files[0].path]));
	f.state.failAt = 'repos/microsoft/winget-pkgs/pulls/123';
	await assert.rejects(f.submit(), /Concurrent.*could not withdraw.*HTTP 503.*lock has been retained/);
	assert.ok(f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
	assert.ok(f.state.refs.has('refs/tags/actionlint-winget-attempt-1.2.3'));
	assert.equal(f.state.issues.length, 1);
	assert.match(f.state.issues[0].body, /pull\/99/);
	assert.match(f.state.issues[0].body, /pull\/123/);
	assert.equal(f.state.calls.filter(c => c.method === 'DELETE').length, 0);
});

test('WinGet lock cleanup failure does not hide the original blocked submission', async t => {
	const f = await wingetFixture(t);
	f.state.pulls.push(f.pull(99, [f.files[0].path]));
	f.state.failAt = 'repos/kjanat/winget-pkgs/git/refs/heads/actionlint-winget-submission-lock';
	await assert.rejects(f.submit(), /blocked.*pull\/99.*could not release.*HTTP 503/);
	assert.ok(f.state.refs.has('refs/heads/actionlint-winget-submission-lock'));
	assert.equal(f.creationCalls().length, 0);
	assert.match(f.state.issues[0].body, /pull\/99/);
});

test('WinGet fails closed on incomplete PR lists and GraphQL errors', async t => {
	for (const scenario of ['incomplete', 'GraphQL']) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			if (scenario === 'incomplete') f.state.incomplete = true;
			else f.state.graphQLError = true;
			await assert.rejects(f.submit(), /Incomplete|GraphQL/);
			assert.equal(f.creationCalls().length, 0);
			assert.equal(f.state.issues.length, 1);
		});
	}
});

test('WinGet rejects malformed and inconsistent PR/file pagination before submission', async t => {
	for (
		const scenario of [
			'repeated PR',
			'repeated file',
			'missing path',
			'incomplete files',
			'empty page',
			'changed count',
			'repeated cursor',
		]
	) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			f.state.pulls.push(f.pull(99, ['unrelated/a', 'unrelated/b']));
			if (scenario === 'repeated PR') f.state.pulls.push(f.pull(99, ['unrelated/c']));
			if (scenario === 'changed count' || scenario === 'repeated cursor') {
				for (let i = 0; i < 50; i++) f.state.pulls.push(f.pull(1000 + i, ['unrelated/c']));
			}
			let pages = 0;
			/** @type {typeof fetch} */
			const broken = async (url, options) => {
				const response = await f.request(url, options);
				if (String(url) !== 'https://api.github.com/graphql') return response;
				const data = await response.json();
				const pulls = data.data.repository.pullRequests;
				pages++;
				if (scenario === 'repeated file') pulls.nodes[0].files.nodes[1].path = 'unrelated/a';
				if (scenario === 'missing path') delete pulls.nodes[0].files.nodes[0].path;
				if (scenario === 'incomplete files') pulls.nodes[0].files.totalCount++;
				if (scenario === 'empty page') {
					pulls.nodes = [];
					pulls.pageInfo.hasNextPage = true;
				}
				if (pages === 2 && scenario === 'changed count') pulls.totalCount++;
				if (pages === 2 && scenario === 'repeated cursor') {
					pulls.pageInfo = { hasNextPage: true, endCursor: '50' };
				}
				return Response.json(data);
			};
			await assert.rejects(
				publishWingetAndReport(f.root, '1.2.3', f.env, broken),
				/Repeated|Invalid|Incomplete|changed/,
			);
			assert.equal(f.creationCalls().length, 0);
			assert.equal(f.state.issues.length, 1);
		});
	}
});

test('WinGet reports input and API failures, including failed PR creation and failed lock cleanup', async t => {
	for (const scenario of ['missing token', 'missing file', 'API failure', 'PR failure', 'cleanup failure']) {
		await t.test(scenario, async t => {
			const f = await wingetFixture(t);
			if (scenario === 'missing token') delete f.env.WINGET_TOKEN;
			if (scenario === 'missing file') await rm(join(f.root, basename(f.files[0].path)));
			if (scenario === 'API failure') {
				f.state.failAt = `repos/microsoft/winget-pkgs/contents/${f.files[0].path}?ref=master`;
			}
			if (scenario === 'PR failure') f.state.failAt = 'repos/microsoft/winget-pkgs/pulls';
			if (scenario === 'cleanup failure') {
				f.state.failAt = 'repos/kjanat/winget-pkgs/git/refs/heads/actionlint-winget-submission-lock';
			}
			await assert.rejects(f.submit());
			assert.equal(f.state.issues.length, 1);
			if (scenario === 'PR failure') {
				f.state.failAt = '';
				await assert.rejects(f.submit(), /already attempted/);
				assert.equal(f.creationCalls().length, 1);
			}
		});
	}
});

test('WinGet failure reporting paginates, excludes PRs, reopens a closed issue and redacts tokens', async t => {
	const f = await wingetFixture(t);
	for (let i = 0; i < 100; i++) f.state.issues.push({ number: i + 1, state: 'open', body: 'unrelated' });
	const marker = '<!-- actionlint-winget-failure:1.2.3 -->';
	f.state.issues[0] = { number: 1, state: 'open', body: marker, pull_request: {} };
	f.state.issues.push({ number: 101, state: 'closed', body: marker });
	await reportWingetFailure('1.2.3', new Error('Failed test-winget and test-issues'), f.env, f.request);
	assert.equal(f.state.issues.length, 101);
	assert.equal(f.state.issues[100].state, 'open');
	assert.match(f.state.issues[100].body, /\[redacted\]/);
	assert.doesNotMatch(f.state.issues[100].body, /test-winget|test-issues/);
	await reportWingetFailure('1.2.3', 'Fallback reporting job', f.env, f.request);
	assert.doesNotMatch(f.state.issues[100].body, /Fallback/);
});

test('WinGet preserves both errors if failure issue creation is unavailable', async t => {
	const f = await wingetFixture(t);
	delete f.env.WINGET_TOKEN;
	f.state.failAt = 'repos/kjanat/actionlint/issues';
	await assert.rejects(f.submit(), /Missing WINGET_TOKEN.*issue could not be created.*HTTP 503/);
	assert.equal(f.creationCalls().length, 0);
	assert.equal(f.state.issues.length, 0);
});
