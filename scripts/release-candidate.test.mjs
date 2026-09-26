import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

import { createReleaseCommit, recordRelease } from './build-action-release.mjs';
import {
	bundleName,
	containerAssets,
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
		assets: [
			bundleName,
			'actionlint-action_1.17.1.mjs',
			'actionlint-action_1.17.1_checksums.txt',
			...containerAssets('1.17.1').flatMap((name) => [`${name}.tar`, `${name}.digest`]),
		].map((name) => ({
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

test('release candidates require both container archives and prepared digests', () => {
	const manifest = exampleManifest();
	for (const name of containerAssets(manifest.version).flatMap((name) => [`${name}.tar`, `${name}.digest`])) {
		assert.throws(
			() => parseManifest({ ...manifest, assets: manifest.assets.filter((asset) => asset.name !== name) }),
			/Candidate is missing/,
		);
	}
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
		for (const name of containerAssets(version)) {
			await writeFile(join(assets, `${name}.tar`), 'OCI archive fixture');
			await writeFile(join(assets, `${name}.digest`), `sha256:${'a'.repeat(64)}\n`);
		}
		const manifest = await writeManifest(root, assets, { version, source, candidate }, {
			GITHUB_REPOSITORY: 'kjanat/actionlint',
			GITHUB_RUN_ID: '123',
			GITHUB_RUN_ATTEMPT: '2',
		});
		assert.deepEqual(JSON.parse(await readFile(join(assets, manifestName), 'utf8')), manifest);
		await verifyCandidate(root, assets, manifest);
		for (const name of containerAssets(version)) {
			const archive = join(assets, `${name}.tar`);
			await writeFile(archive, 'changed OCI archive');
			await assert.rejects(verifyAssets(assets, manifest), /Candidate asset changed/);
			await writeFile(archive, 'OCI archive fixture');
			const digestName = `${name}.digest`;
			await writeFile(join(assets, digestName), 'not-an-image-digest');
			const altered = {
				...manifest,
				assets: manifest.assets.map((asset) =>
					asset.name === digestName
						? { ...asset, size: 19, sha256: createHash('sha256').update('not-an-image-digest').digest('hex') }
						: asset
				),
			};
			await assert.rejects(verifyAssets(assets, altered), /Invalid prepared container digest/);
			await writeFile(join(assets, digestName), `sha256:${'a'.repeat(64)}\n`);
		}
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
		failSign: false,
		failRecord: false,
		failPush: false,
		pushAccepted: false,
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
				events.push('sign');
				if (state.failSign) throw new Error('Signing interrupted');
				state.localTag = signedTag;
				return '';
			}
			if (cmd === '-c' && args.includes('push')) {
				events.push('push');
				if (state.pushAccepted || !state.failPush) {
					state.remoteTag = state.localTag;
					state.remoteHead = state.head;
				}
				if (state.failPush) throw new Error('Push interrupted');
				return '';
			}
			throw new Error(`Unexpected fixture Git call: ${args.join(' ')}`);
		},
		record: () => {
			events.push('record');
			if (state.failRecord) throw new Error('Recording interrupted');
			state.head = merge;
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

test('promotion resumes a signing interruption before any remote writes', () => {
	const fixture = promotionFixture();
	fixture.state.failSign = true;
	assert.throws(
		() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations),
		/Signing interrupted/,
	);
	assert.deepEqual(fixture.events, ['sign']);
	assert.equal(fixture.state.remoteTag, '');
	fixture.state.failSign = false;
	fixture.events.length = 0;
	finalizePromotion(fixture.manifest, fixture.release(), fixture.operations);
	assert.deepEqual(fixture.events, ['sign', 'record', 'push', 'publish']);
});

test('promotion reuses a signed tag after recording failed', () => {
	const fixture = promotionFixture();
	fixture.state.failRecord = true;
	assert.throws(
		() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations),
		/signed tag exists locally/,
	);
	assert.deepEqual(fixture.events, ['sign', 'record']);
	assert.equal(fixture.state.remoteTag, '');
	fixture.state.failRecord = false;
	fixture.events.length = 0;
	finalizePromotion(fixture.manifest, fixture.release(), fixture.operations);
	assert.deepEqual(fixture.events, ['record', 'push', 'publish']);
});

test('promotion verifies and resumes an interrupted atomic push', () => {
	for (const accepted of [false, true]) {
		const fixture = promotionFixture();
		fixture.state.failPush = true;
		fixture.state.pushAccepted = accepted;
		assert.throws(
			() => finalizePromotion(fixture.manifest, fixture.release(), fixture.operations),
			/verify or resume the atomic push/,
		);
		assert.deepEqual(fixture.events, ['sign', 'record', 'push']);
		assert.equal(fixture.state.draft, true);
		fixture.state.failPush = false;
		fixture.events.length = 0;
		finalizePromotion(fixture.manifest, fixture.release(), fixture.operations);
		assert.deepEqual(fixture.events, ['push', 'publish']);
	}
});

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

test('real signed promotion survives rejected pushes and lost responses without changing bytes', async (t) => {
	const gpgconf = spawnSync('gpgconf', ['--list-components'], { encoding: 'utf8', timeout: 5_000 });
	const component = gpgconf.stdout?.split(/\r?\n/).find((line) => line.startsWith('gpg:'))?.split(':')[2];
	if (gpgconf.error || gpgconf.status !== 0 || !component) {
		t.skip('GnuPG with Ed25519 and gpgconf required for isolated signing');
		return;
	}
	// Git for Windows prepends its own tools to PATH; use the keyring's GnuPG executable.
	const gpgProgram = decodeURIComponent(component).replaceAll('\\', '/');
	const gpg = spawnSync(gpgProgram, ['--version'], { encoding: 'utf8', timeout: 5_000 });
	if (gpg.error || gpg.status !== 0 || !gpg.stdout.includes('EDDSA')) {
		t.skip('GnuPG lacks Ed25519 support');
		return;
	}
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-real-promotion-'));
	const keyring = join(temporary, 'keyring');
	const root = join(temporary, 'source');
	const remote = join(temporary, 'remote.git');
	const env = { ...process.env, GNUPGHOME: keyring };
	/** @param {string} cwd @param {string[]} args */
	const git = (cwd, ...args) =>
		execFileSync('git', args, { cwd, env, encoding: 'utf8', timeout: 10_000, stdio: 'pipe' }).trim();
	try {
		await mkdir(keyring, { mode: 0o700 });
		await mkdir(root);
		execFileSync(gpgProgram, [
			'--batch',
			'--pinentry-mode',
			'loopback',
			'--passphrase',
			'',
			'--quick-generate-key',
			'Release fixture <release@example.invalid>',
			'ed25519',
			'sign',
			'0',
		], { env, timeout: 10_000, stdio: 'pipe' });
		const keys = execFileSync(gpgProgram, ['--batch', '--with-colons', '--list-secret-keys'], {
			env,
			encoding: 'utf8',
			timeout: 5_000,
			stdio: 'pipe',
		});
		const fingerprint = keys.split(/\r?\n/).find((line) => line.startsWith('fpr:'))?.split(':')[9];
		assert.ok(fingerprint, 'missing isolated signing key');
		/** @param {string} cwd */
		const configure = (cwd) => {
			for (
				const [name, value] of Object.entries({
					'user.name': 'Release fixture',
					'user.email': 'release@example.invalid',
					'user.signingkey': fingerprint,
					'gpg.program': gpgProgram,
					'commit.gpgsign': 'false',
					'core.autocrlf': 'false',
					'core.hooksPath': join(temporary, 'empty-hooks'),
				})
			) git(cwd, 'config', name, value);
		};
		git(root, 'init', '--quiet', '--initial-branch=master');
		configure(root);
		const sourceFiles = {
			'action.yml': 'runs: {using: node24, main: action.mjs}\n',
			'README.md': '# Promotion fixture\n',
			'LICENSE.txt': 'Fixture license\n',
		};
		for (const [name, content] of Object.entries(sourceFiles)) await writeFile(join(root, name), content);
		git(root, 'add', '.');
		git(root, 'commit', '--quiet', '-m', 'Prepared source');
		const source = git(root, 'rev-parse', 'HEAD');
		git(root, 'init', '--quiet', '--bare', '--initial-branch=master', remote);
		git(root, 'remote', 'add', 'origin', remote);
		git(root, 'push', '--quiet', 'origin', 'master');
		const directory = join(temporary, 'prepared');
		const action = join(directory, 'action');
		await mkdir(action, { recursive: true });
		const bundle = Buffer.from('console.log("candidate bytes: π");\n');
		const checksum = Buffer.from(`${createHash('sha256').update(bundle).digest('hex')}  action.mjs\n`);
		for (const [name, content] of Object.entries(sourceFiles)) await writeFile(join(action, name), content);
		await writeFile(join(action, 'action.mjs'), bundle);
		await writeFile(join(action, 'SHA256SUMS'), checksum);
		const candidate = await createReleaseCommit(root, directory, '1.17.1', source);
		const manifest = { ...exampleManifest(), source, candidate, tree: git(root, 'rev-parse', `${candidate}^{tree}`) };
		const hook = join(remote, 'hooks', 'update');
		// Reject only the tag: --atomic must also leave the otherwise accepted branch unchanged.
		await writeFile(hook, '#!/bin/sh\ncase "$1" in refs/tags/*) exit 1 ;; *) exit 0 ;; esac\n', { mode: 0o755 });
		git(remote, 'config', 'core.hooksPath', join(remote, 'hooks'));
		let draft = true;
		let losePushResponse = false;
		let acceptPublication = false;
		let signCount = 0;
		let recordCount = 0;
		let publishCount = 0;
		/** @param {string} cwd @returns {import('./release-candidate.mjs').PromotionOperations} */
		const operations = (cwd) => ({
			git: (...args) => {
				if (args.includes('-s') && args.includes('tag')) signCount++;
				const result = git(cwd, ...args);
				if (args.includes('push') && losePushResponse) {
					losePushResponse = false;
					throw new Error('Push response lost after acceptance');
				}
				return result;
			},
			record: (tag, parent) => {
				recordCount++;
				recordRelease(cwd, tag, parent);
			},
			publish: () => {
				publishCount++;
				if (acceptPublication) draft = false;
				throw new Error('Publication response lost');
			},
		});
		const release = () => ({ id: 789, draft, tag_name: manifest.tag, target_commitish: source });
		assert.throws(() => finalizePromotion(manifest, release(), operations(root)), /verify or resume the atomic push/);
		const tagObject = git(root, 'rev-parse', `refs/tags/${manifest.tag}`);
		const recorded = git(root, 'rev-parse', 'HEAD');
		assert.equal(git(remote, 'tag', '--list'), '');
		assert.equal(git(remote, 'rev-parse', 'master'), source);
		assert.equal(publishCount, 0);
		await rm(hook);
		losePushResponse = true;
		assert.throws(() => finalizePromotion(manifest, release(), operations(root)), /verify or resume the atomic push/);
		assert.equal(git(remote, 'rev-parse', 'master'), recorded);
		assert.equal(git(remote, 'rev-parse', `refs/tags/${manifest.tag}`), tagObject);
		assert.throws(() => finalizePromotion(manifest, release(), operations(root)), /resume draft publication/);
		assert.equal(draft, true);
		const resumed = join(temporary, 'resumed');
		git(root, 'clone', '--quiet', remote, resumed);
		configure(resumed);
		acceptPublication = true;
		assert.throws(() => finalizePromotion(manifest, release(), operations(resumed)), /resume draft publication/);
		assert.equal(finalizePromotion(manifest, release(), operations(resumed)).alreadyPublished, true);
		assert.equal(signCount, 1);
		assert.equal(recordCount, 1);
		assert.equal(publishCount, 2);
		assert.equal(git(remote, 'rev-parse', `refs/tags/${manifest.tag}`), tagObject);
		assert.equal(git(remote, 'rev-parse', `${manifest.tag}^{commit}`), candidate);
		assert.equal(git(remote, 'rev-list', '--parents', '-n', '1', 'master'), `${recorded} ${source} ${candidate}`);
		assert.equal(git(remote, 'rev-parse', 'master^{tree}'), git(root, 'rev-parse', `${source}^{tree}`));
		git(resumed, 'verify-tag', manifest.tag);
		for (const [name, bytes] of Object.entries({ 'action.mjs': bundle, SHA256SUMS: checksum })) {
			assert.deepEqual(execFileSync('git', ['show', `${manifest.tag}:${name}`], { cwd: remote, env }), bytes);
			assert.deepEqual(await readFile(join(action, name)), bytes);
		}
	} finally {
		const stopped = spawnSync('gpgconf', ['--homedir', keyring, '--kill', 'gpg-agent'], {
			env,
			encoding: 'utf8',
			timeout: 5_000,
		});
		await rm(temporary, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
		assert.ifError(stopped.error);
		assert.equal(stopped.status, 0, stopped.stderr);
	}
});

// Execute the workflow's actual Bash gate. These functions replace both external
// commands; jq evaluates the real selectors against mock paginated API responses.
const manualNpmCommands = `
git() { printf '%s\\n' "$MOCK_COMMIT"; }
gh() {
  if [[ "$1" == release ]]; then
    printf '%s\\n' "$MOCK_PUBLISHED"
    return
  fi
  local endpoint='' selector='' status='' event='' head='' paginate=false
  shift
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --method) shift 2 ;;
      --paginate) paginate=true; shift ;;
      --jq) selector="$2"; shift 2 ;;
      --field)
        case "$2" in
          status=*) status="\${2#status=}" ;;
          event=*) event="\${2#event=}" ;;
          head_sha=*) head="\${2#head_sha=}" ;;
          per_page=100|filter=latest) ;;
          *) return 2 ;;
        esac
        shift 2 ;;
      repos/*) endpoint="$1"; shift ;;
      *) return 2 ;;
    esac
  done
  [[ "$paginate" == true ]] || return 2
  case "$endpoint" in
    repos/kjanat/actionlint/actions/workflows/release.yml/runs)
      jq -c --arg status "$status" --arg event "$event" --arg head "$head" '
        .workflow_runs |= map(select(
          ($status == "" or .status == $status or .conclusion == $status) and
          ($event == "" or .event == $event) and ($head == "" or .head_sha == $head)
        ))' <<< "$MOCK_RUN_PAGES" | jq -r "$selector"
      ;;
    repos/kjanat/actionlint/actions/runs/*/jobs)
      [[ "$MOCK_JOBS_ERROR" == false ]] || return 1
      local run_id="\${endpoint%/jobs}"
      run_id="\${run_id##*/}"
      jq -c --arg id "$run_id" '.[$id][]' <<< "$MOCK_JOB_PAGES" | jq -r "$selector"
      ;;
    *) return 2 ;;
  esac
}
`;

const verifiedReleaseRun = {
	id: 101,
	event: 'release',
	head_branch: 'v1.17.1',
	head_sha: '2'.repeat(40),
	status: 'completed',
	conclusion: 'failure', // npm failed after candidate verification succeeded.
};
const verifiedReleaseJob = { id: 201, name: 'Verify published candidate', status: 'completed', conclusion: 'success' };

for (
	const scenario of [
		{ name: 'recovers after npm failed', accepted: true },
		{ name: 'accepts a successful Release run', run: { conclusion: 'success' }, accepted: true },
		{ name: 'rejects a running Release run', run: { status: 'in_progress' } },
		{ name: 'rejects a different tag', run: { head_branch: 'v1.17.2' } },
		{ name: 'rejects a different commit', run: { head_sha: '3'.repeat(40) } },
		{ name: 'rejects a different event', run: { event: 'workflow_dispatch', conclusion: 'success' } },
		{ name: 'rejects failed verification', run: { conclusion: 'success' }, job: { conclusion: 'failure' } },
		{ name: 'rejects skipped verification', run: { conclusion: 'success' }, job: { conclusion: 'skipped' } },
		{
			name: 'rejects incomplete verification',
			run: { conclusion: 'success' },
			job: { status: 'in_progress', conclusion: null },
		},
		{ name: 'rejects another successful job', run: { conclusion: 'success' }, job: { name: 'Publish npm packages' } },
		{ name: 'rejects a missing verification job', run: { conclusion: 'success' }, missingJob: true },
		{ name: 'rejects missing Release runs', missingRun: true },
		{ name: 'rejects an unpublished release', published: false },
		{ name: 'rejects a jobs API failure', run: { conclusion: 'success' }, jobsError: true },
	]
) {
	test(`manual npm publishing ${scenario.name}`, async () => {
		const workflow = await readFile(new URL('../.github/workflows/npm-release.yml', import.meta.url), 'utf8');
		const match = workflow.match(
			/ {6}- name: Verify release before manual publishing\r?\n[\s\S]*? {8}run: \|\r?\n((?: {10}.*\r?\n)+)/,
		);
		assert.ok(match, 'manual publishing gate must remain covered');
		const script = match[1].split(/\r?\n/).map((line) => line.slice(10)).join('\n');
		// Empty first pages exercise gh's multi-page output for both endpoints.
		const runPages = [{ workflow_runs: [] }, {
			workflow_runs: scenario.missingRun ? [] : [{ ...verifiedReleaseRun, ...scenario.run }],
		}];
		const jobPages = {
			101: [{ jobs: [] }, {
				jobs: scenario.missingJob ? [] : [{ ...verifiedReleaseJob, ...scenario.job }],
			}],
		};
		const result = spawnSync('bash', ['--noprofile', '--norc', '-s'], {
			input: `${manualNpmCommands}\n${script}`,
			encoding: 'utf8',
			env: {
				...process.env,
				GITHUB_REPOSITORY: 'kjanat/actionlint',
				RELEASE_TAG: 'v1.17.1',
				MOCK_COMMIT: verifiedReleaseRun.head_sha,
				MOCK_PUBLISHED: String(scenario.published !== false),
				MOCK_RUN_PAGES: runPages.map((page) => JSON.stringify(page)).join('\n'),
				MOCK_JOB_PAGES: JSON.stringify(jobPages),
				MOCK_JOBS_ERROR: String(scenario.jobsError === true),
			},
		});
		assert.ifError(result.error);
		if (scenario.accepted) assert.equal(result.status, 0, result.stdout + result.stderr);
		else assert.equal(result.status, 1, result.stdout + result.stderr);
	});
}
