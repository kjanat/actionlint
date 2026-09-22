import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

import {
	bundleName,
	finalizePromotion,
	findRelease,
	manifestName,
	parseManifest,
	prepareCandidate,
	verifyAssets,
	verifyCandidate,
	verifyRun,
	workflowPath,
	writeManifest,
} from './release-candidate.mjs';

test('pending draft tags resolve through gh before fetching the release by ID', () => {
	const release = { id: 77, tag_name: 'v1.17.1', draft: true };
	/** @type {string[][]} */
	const calls = [];
	const found = findRelease('kjanat/actionlint', 'v1.17.1', (args) => {
		calls.push(args);
		if (args[0] === 'release') return '77';
		assert.deepEqual(args, ['api', 'repos/kjanat/actionlint/releases/77']);
		return JSON.stringify(release);
	});
	assert.deepEqual(found, release);
	assert.deepEqual(calls[0], [
		'release',
		'view',
		'v1.17.1',
		'--repo',
		'kjanat/actionlint',
		'--json',
		'databaseId',
		'--jq',
		'.databaseId',
	]);
	assert.throws(() => findRelease('kjanat/actionlint', 'v1.17.1', () => 'null'), /release ID/);
});

function exampleManifest() {
	return parseManifest({
		schema_version: 1,
		repository: 'kjanat/actionlint',
		version: '1.17.1',
		tag: 'v1.17.1',
		workflow: workflowPath,
		run_id: 123,
		run_attempt: 2,
		source: '1'.repeat(40),
		candidate: '2'.repeat(40),
		tree: '3'.repeat(40),
		bundle: bundleName,
		assets: [bundleName, 'actionlint-action_1.17.1.mjs', 'actionlint-action_1.17.1_checksums.txt'].map((name) => ({
			name,
			size: 10,
			sha256: '4'.repeat(64),
		})),
	});
}

test('candidate provenance requires the successful exact workflow attempt and source', () => {
	const manifest = exampleManifest();
	const run = {
		id: 123,
		run_attempt: 2,
		event: 'workflow_dispatch',
		path: workflowPath,
		head_branch: 'master',
		head_sha: manifest.source,
		repository: { full_name: 'kjanat/actionlint' },
		status: 'completed',
		conclusion: 'success',
	};
	verifyRun(run, manifest);
	for (
		const patch of [
			{ id: 124 },
			{ run_attempt: 3 },
			{ event: 'pull_request' },
			{ path: '.github/workflows/ci.yml' },
			{ head_branch: 'feature' },
			{ head_sha: '5'.repeat(40) },
			{ repository: { full_name: 'other/actionlint' } },
			{ status: 'in_progress' },
			{ conclusion: 'failure' },
		]
	) assert.throws(() => verifyRun({ ...run, ...patch }, manifest), /successful release preparation/);
	assert.throws(() => parseManifest({ ...manifest, version: '01.17.1' }), /stable version/);
	assert.throws(() => parseManifest({ ...manifest, assets: [...manifest.assets, manifest.assets[0]] }), /Duplicate/);
	assert.throws(
		() => parseManifest({ ...manifest, assets: [{ name: '../escape', size: 0, sha256: '4'.repeat(64) }] }),
		/filename/,
	);
});

test('manifest binds exact assets to the complete candidate tree', async () => {
	const tempRoot = resolve(tmpdir());
	const temporary = await mkdtemp(join(tempRoot, 'actionlint-candidate-test-'));
	try {
		const root = join(temporary, 'source');
		const directory = join(temporary, 'release');
		const assets = join(directory, 'assets');
		await mkdir(root);
		/** @param {string[]} args */
		const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
		git('init', '--quiet');
		git('config', 'user.name', 'Candidate fixture');
		git('config', 'user.email', 'candidate@example.invalid');
		git('config', 'commit.gpgsign', 'false');
		git('config', 'tag.gpgsign', 'true');
		git('config', 'core.autocrlf', 'true');
		const code = 'console.log("candidate");\n';
		const sources = {
			'.gitattributes': '* text=auto eol=lf\n',
			'action.yml': 'runs: { using: node24, main: action.mjs }\n',
			'README.md': '# Fixture\n',
			'LICENSE.txt': 'Fixture\n',
			'go.mod': 'module example.invalid/actionlint\n',
			'packages/github-action/package.json': JSON.stringify({ scripts: { build: 'node build.mjs' } }),
			'packages/github-action/build.mjs': `
import { mkdir, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { join } from 'node:path';
const directory = process.argv[process.argv.indexOf('--out-dir') + 1];
const code = ${JSON.stringify(code)};
await mkdir(directory, { recursive: true });
await writeFile(join(directory, 'action.mjs'), code);
await writeFile(join(directory, 'SHA256SUMS'), createHash('sha256').update(code).digest('hex') + '  action.mjs\\n');
`,
		};
		for (const [name, content] of Object.entries(sources)) {
			await mkdir(dirname(join(root, name)), { recursive: true });
			await writeFile(join(root, name), content);
		}
		git('add', '.');
		git('commit', '--quiet', '-m', 'Source fixture');
		const source = git('rev-parse', 'HEAD');
		const version = '1.17.1';
		const metadata = await prepareCandidate(root, directory, version);
		const { candidate } = metadata;
		assert.deepEqual(JSON.parse(await readFile(join(directory, 'candidate.json'), 'utf8')), metadata);
		assert.equal(git('rev-parse', 'HEAD'), source);
		assert.equal(git('status', '--porcelain'), '');
		assert.equal(git('rev-parse', `v${version}^{commit}`), candidate);
		assert.equal(git('cat-file', '-t', `v${version}`), 'tag');
		assert.doesNotMatch(git('cat-file', '-p', `v${version}`), /BEGIN PGP SIGNATURE/);
		const ref = `refs/actionlint/candidates/v${version}`;
		assert.equal(git('for-each-ref', '--format=%(refname)', ref), '');
		const manifest = await writeManifest(root, assets, { version, source, candidate }, {
			GITHUB_REPOSITORY: 'kjanat/actionlint',
			GITHUB_RUN_ID: '123',
			GITHUB_RUN_ATTEMPT: '2',
		});
		assert.deepEqual(JSON.parse(await readFile(join(assets, manifestName), 'utf8')), manifest);
		await verifyCandidate(root, assets, manifest);
		await assert.rejects(verifyCandidate(root, assets, { ...manifest, source: 'f'.repeat(40) }), /source parent/);
		await assert.rejects(verifyCandidate(root, assets, { ...manifest, tree: 'f'.repeat(40) }), /tree differs/);
		await writeFile(join(assets, 'unexpected.txt'), 'extra');
		await assert.rejects(verifyAssets(assets, manifest), /asset set differs/);
		await rm(join(assets, 'unexpected.txt'));
		await writeFile(join(assets, `actionlint-action_${version}.mjs`), 'changed');
		await assert.rejects(verifyAssets(assets, manifest), /asset changed/);
		await writeFile(join(assets, `actionlint-action_${version}.mjs`), code);
		await writeFile(join(root, 'go.mod'), 'module changed.invalid/source\n');
		git('add', 'go.mod');
		const changedTree = git('write-tree');
		const changed = git('commit-tree', changedTree, '-p', source, '-m', 'Changed source');
		git('update-ref', ref, changed);
		git('bundle', 'create', join(assets, bundleName), ref, `^${source}`);
		const changedBytes = await readFile(join(assets, bundleName));
		const altered = {
			...manifest,
			candidate: changed,
			tree: changedTree,
			assets: manifest.assets.map((asset) =>
				asset.name === bundleName
					? { ...asset, size: changedBytes.length, sha256: createHash('sha256').update(changedBytes).digest('hex') }
					: asset
			),
		};
		await assert.rejects(verifyCandidate(root, assets, altered), /changed source files/);
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
});

function promotionFixture() {
	const manifest = exampleManifest();
	const merge = '5'.repeat(40);
	const signedTag = '6'.repeat(40);
	const state = {
		head: manifest.source,
		remoteHead: manifest.source,
		localTag: '',
		remoteTag: '',
		draft: true,
		failPublish: false,
		publishAccepted: false,
	};
	/** @type {string[]} */
	const events = [];
	/** @type {import('./release-candidate.mjs').PromotionOperations} */
	const operations = {
		git: (...args) => {
			const [cmd, ...rest] = args;
			if (cmd === 'ls-remote') {
				return rest[1] === 'refs/heads/master'
					? `${state.remoteHead}\trefs/heads/master`
					: state.remoteTag && `${state.remoteTag}\trefs/tags/${manifest.tag}`;
			}
			if (cmd === 'tag') return state.localTag ? manifest.tag : '';
			if (cmd === 'rev-parse') {
				if (rest[0] === 'HEAD') return state.head;
				if (rest[0].endsWith('^{tree}')) return manifest.tree;
				if (rest[0].endsWith('^{commit}')) return manifest.candidate;
				return state.localTag;
			}
			if (cmd === 'rev-list') {
				return rest.at(-1) === merge
					? `${merge} ${manifest.source} ${manifest.candidate}`
					: 'unrelated history';
			}
			if (cmd === 'cat-file') return rest[0] === '-t' ? 'tag' : '-----BEGIN PGP SIGNATURE-----';
			if (cmd === 'verify-tag' || cmd === 'status') return '';
			if (cmd === 'branch') return 'master';
			if (cmd === 'fetch') {
				state.localTag = state.remoteTag;
				return '';
			}
			if (cmd === 'merge') {
				state.head = state.remoteHead;
				events.push('fast-forward');
				return '';
			}
			if (cmd === '-c' && args.includes('tag')) {
				state.localTag = signedTag;
				events.push('sign');
				return '';
			}
			if (cmd === '-c' && args.includes('push')) {
				state.remoteTag = state.localTag;
				state.remoteHead = state.head;
				events.push('push');
				return '';
			}
			throw new Error(`Unexpected fixture Git call: ${args.join(' ')}`);
		},
		record: () => {
			state.head = merge;
			events.push('record');
		},
		publish: (id) => {
			events.push('publish');
			if (state.publishAccepted || !state.failPublish) state.draft = false;
			if (state.failPublish) throw new Error('Publication response lost');
			return { id, draft: false, tag_name: manifest.tag };
		},
	};
	const release = () => ({ id: 789, draft: state.draft, tag_name: manifest.tag, target_commitish: manifest.source });
	return { manifest, state, events, operations, release };
}

test('promotion resumes after push without signing or recording a second release', () => {
	for (const accepted of [false, true]) {
		const fixture = promotionFixture();
		fixture.state.failPublish = true;
		fixture.state.publishAccepted = accepted;
		assert.throws(
			() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations),
			/signed tag was pushed/,
		);
		assert.deepEqual(fixture.events, ['sign', 'record', 'push', 'publish']);
		fixture.events.length = 0;
		fixture.state.failPublish = false;
		const result = finalizePromotion(fixture.manifest, fixture.release(), fixture.operations);
		assert.equal(result.alreadyPublished, accepted);
		assert.deepEqual(fixture.events, accepted ? [] : ['push', 'publish']);
	}
});

test('promotion refuses changed source and conflicting immutable tag before writes', () => {
	const fixture = promotionFixture();
	fixture.state.head = '7'.repeat(40);
	assert.throws(() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations), /Source advanced/);
	assert.deepEqual(fixture.events, []);
	fixture.state.head = fixture.manifest.source;
	fixture.state.localTag = '6'.repeat(40);
	fixture.state.remoteTag = '7'.repeat(40);
	assert.throws(
		() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations),
		/will not be replaced/,
	);
	assert.deepEqual(fixture.events, []);
});

test('another checkout can resume an already pushed ancestry commit', () => {
	const fixture = promotionFixture();
	fixture.state.failPublish = true;
	assert.throws(() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations));
	fixture.state.head = fixture.manifest.source;
	fixture.state.localTag = '';
	fixture.events.length = 0;
	fixture.state.failPublish = false;
	finalizePromotion(fixture.manifest, fixture.release(), fixture.operations);
	assert.deepEqual(fixture.events, ['fast-forward', 'push', 'publish']);
});
