import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { closeSync, createReadStream, openSync } from 'node:fs';
import { appendFile, lstat, mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

import { createReleaseCommit, prepareRelease, recordRelease, sourceRoot } from './build-action-release.mjs';

export const workflowPath = '.github/workflows/release-prepare.yml';
export const manifestName = 'release-candidate.json';
export const bundleName = 'release-candidate.bundle';

/** @typedef {{name: string, size: number, sha256: string}} CandidateAsset */
/** @typedef {{version: string, source: string, candidate: string, tree: string}} CandidateMetadata */
/** @typedef {CandidateMetadata & {schema_version: number, repository: string, tag: string, workflow: string, run_id: number, run_attempt: number, bundle: string, assets: CandidateAsset[]}} CandidateManifest */
/** @typedef {(...args: string[]) => string} GitCommand */
/** @typedef {{git: GitCommand, record: (tag: string, source: string) => void, publish: (id: number) => unknown}} PromotionOperations */

/** @param {string} program @param {string[]} args @param {string} root @param {Omit<import('node:child_process').SpawnSyncOptionsWithStringEncoding, 'encoding'>} [options] */
function command(program, args, root, options = {}) {
	const result = spawnSync(program, args, { cwd: root, encoding: 'utf8', ...options });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${program} ${args[0]} failed: ${result.stderr || result.stdout}`);
	return result.stdout?.trim() || '';
}

/** @param {string} root @param {string[]} args */
function git(root, ...args) {
	return command('git', args, root);
}

/** @param {string | Uint8Array} bytes */
function sha256(bytes) {
	return createHash('sha256').update(bytes).digest('hex');
}

/** @param {string} path */
async function hashAsset(path) {
	if (!(await lstat(path)).isFile()) throw new Error(`Candidate asset is not a regular file: ${path}`);
	const hash = createHash('sha256');
	let size = 0;
	for await (const chunk of createReadStream(path)) {
		hash.update(chunk);
		size += chunk.length;
	}
	return { size, sha256: hash.digest('hex') };
}

/** @param {unknown} value @param {string} label @returns {Record<string, unknown>} */
function object(value, label) {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`Invalid ${label}`);
	return Object.fromEntries(Object.entries(value));
}

/** @param {unknown} value @param {RegExp} pattern @param {string} label */
function string(value, pattern, label) {
	if (typeof value !== 'string' || !pattern.test(value)) throw new Error(`Invalid ${label}`);
	return value;
}

/** @param {unknown} value @param {string} label @param {number} [minimum] */
function integer(value, label, minimum = 1) {
	if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < minimum) throw new Error(`Invalid ${label}`);
	return value;
}

/** @param {unknown} value */
function versionValue(value) {
	return string(value, /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/, 'stable version');
}

/** @param {unknown} value */
function commitValue(value) {
	return string(value, /^[a-f0-9]{40}$/, 'commit SHA');
}

/** @param {unknown} value */
function assetName(value) {
	return string(value, /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/, 'asset filename');
}

/** @param {string} version */
function candidateRef(version) {
	return `refs/actionlint/candidates/v${versionValue(version)}`;
}

/** @param {unknown} value @returns {CandidateManifest} */
export function parseManifest(value) {
	const data = object(value, 'candidate manifest');
	if (data.schema_version !== 1) throw new Error('Unsupported candidate manifest version');
	const version = versionValue(data.version);
	if (data.tag !== `v${version}` || data.workflow !== workflowPath || data.bundle !== bundleName) {
		throw new Error('Candidate tag, workflow or bundle does not match');
	}
	if (!Array.isArray(data.assets)) throw new Error('Candidate assets must be an array');
	const assets = data.assets.map((value) => {
		const asset = object(value, 'asset');
		return {
			name: assetName(asset.name),
			size: integer(asset.size, 'asset size', 0),
			sha256: string(asset.sha256, /^[a-f0-9]{64}$/, 'asset SHA-256'),
		};
	});
	const names = new Set(assets.map((asset) => asset.name));
	if (names.size !== assets.length || names.has(manifestName)) throw new Error('Duplicate or recursive manifest asset');
	for (const name of [bundleName, `actionlint-action_${version}.mjs`, `actionlint-action_${version}_checksums.txt`]) {
		if (!names.has(name)) throw new Error(`Candidate is missing ${name}`);
	}
	return {
		schema_version: 1,
		repository: string(data.repository, /^[\w.-]+\/[\w.-]+$/, 'repository'),
		version,
		tag: `v${version}`,
		workflow: workflowPath,
		run_id: integer(data.run_id, 'workflow run ID'),
		run_attempt: integer(data.run_attempt, 'workflow run attempt'),
		source: commitValue(data.source),
		candidate: commitValue(data.candidate),
		tree: commitValue(data.tree),
		bundle: bundleName,
		assets,
	};
}

/** @param {string} root @param {string} directory @param {string} version */
export async function prepareCandidate(root, directory, version) {
	versionValue(version);
	if (git(root, 'status', '--porcelain', '--untracked-files=all')) {
		throw new Error('Prepare requires a clean source checkout');
	}
	const source = git(root, 'rev-parse', 'HEAD');
	await prepareRelease(root, directory, version);
	const candidate = await createReleaseCommit(root, directory, version, source);
	const tree = git(root, 'rev-parse', `${candidate}^{tree}`);
	const ref = candidateRef(version);
	git(root, 'update-ref', ref, candidate, '');
	try {
		git(root, 'bundle', 'create', join(directory, 'assets', bundleName), ref, `^${source}`);
	} finally {
		git(root, 'update-ref', '-d', ref, candidate);
	}
	// GoReleaser needs a local version tag; only the promotion command signs and pushes it.
	git(
		root,
		'-c',
		'tag.forceSignAnnotated=false',
		'tag',
		'--no-sign',
		'-a',
		'-m',
		`v${version}`,
		`v${version}`,
		candidate,
	);
	const metadata = { version, source, candidate, tree };
	await writeFile(join(directory, 'candidate.json'), `${JSON.stringify(metadata, null, 2)}\n`);
	return metadata;
}

/** @param {string} root @param {string} directory @param {Omit<CandidateMetadata, 'tree'>} metadata @param {NodeJS.ProcessEnv} [environment] */
export async function writeManifest(root, directory, metadata, environment = process.env) {
	const assets = [];
	for (const name of (await readdir(directory)).sort()) {
		assetName(name);
		if (name === manifestName) throw new Error('Candidate manifest already exists');
		assets.push({ name, ...await hashAsset(join(directory, name)) });
	}
	const manifest = parseManifest({
		schema_version: 1,
		repository: environment.GITHUB_REPOSITORY,
		version: metadata.version,
		tag: `v${metadata.version}`,
		workflow: workflowPath,
		run_id: Number(environment.GITHUB_RUN_ID),
		run_attempt: Number(environment.GITHUB_RUN_ATTEMPT),
		source: metadata.source,
		candidate: metadata.candidate,
		tree: git(root, 'rev-parse', `${commitValue(metadata.candidate)}^{tree}`),
		bundle: bundleName,
		assets,
	});
	await verifyCandidate(root, directory, manifest);
	await writeFile(join(directory, manifestName), `${JSON.stringify(manifest, null, 2)}\n`);
	return manifest;
}

/** @param {string} directory @param {CandidateManifest} manifest */
export async function verifyAssets(directory, manifest) {
	const names = (await readdir(directory)).filter((name) => name !== manifestName).sort();
	const expected = manifest.assets.map((asset) => asset.name).sort();
	if (JSON.stringify(names) !== JSON.stringify(expected)) {
		throw new Error('Candidate asset set differs from the manifest');
	}
	for (const asset of manifest.assets) {
		const actual = await hashAsset(join(directory, asset.name));
		if (actual.size !== asset.size || actual.sha256 !== asset.sha256) {
			throw new Error(`Candidate asset changed: ${asset.name}`);
		}
	}
}

/** @param {string} root @param {string} directory @param {CandidateManifest} manifest */
export async function verifyCandidate(root, directory, manifest) {
	await verifyAssets(directory, manifest);
	const bundle = join(directory, bundleName);
	const ref = candidateRef(manifest.version);
	if (git(root, 'bundle', 'list-heads', bundle) !== `${manifest.candidate} ${ref}`) {
		throw new Error('Candidate bundle reference differs');
	}
	git(root, 'bundle', 'verify', bundle);
	git(root, 'fetch', '--no-tags', '--no-write-fetch-head', bundle, ref);
	if (
		git(root, 'rev-list', '--parents', '-n', '1', manifest.candidate) !== `${manifest.candidate} ${manifest.source}`
	) {
		throw new Error('Candidate must have exactly the declared source parent');
	}
	if (git(root, 'rev-parse', `${manifest.candidate}^{tree}`) !== manifest.tree) {
		throw new Error('Candidate tree differs');
	}
	const changed = git(root, 'diff-tree', '--no-commit-id', '--name-status', '-r', manifest.source, manifest.candidate);
	if (changed !== 'A\tSHA256SUMS\nA\taction.mjs') {
		throw new Error('Candidate changed source files or omitted the bundle');
	}
	for (const name of ['action.mjs', 'SHA256SUMS']) {
		if (!git(root, 'ls-tree', manifest.candidate, '--', name).startsWith('100644 blob ')) {
			throw new Error(`Invalid candidate file mode: ${name}`);
		}
	}
	const bundleBytes = spawnSync('git', ['show', `${manifest.candidate}:action.mjs`], {
		cwd: root,
		maxBuffer: 16 * 1024 * 1024,
	});
	if (bundleBytes.error) throw bundleBytes.error;
	if (bundleBytes.status !== 0) throw new Error('Cannot read candidate entrypoint');
	const digest = sha256(bundleBytes.stdout);
	const checksum = git(root, 'show', `${manifest.candidate}:SHA256SUMS`);
	if (checksum !== `${digest}  action.mjs`) throw new Error('Candidate tree checksum differs');
	if (!(await readFile(join(directory, `actionlint-action_${manifest.version}.mjs`))).equals(bundleBytes.stdout)) {
		throw new Error('Published Action asset differs from the candidate tree');
	}
	const assetChecksums = await readFile(join(directory, `actionlint-action_${manifest.version}_checksums.txt`), 'utf8');
	if (assetChecksums !== `${digest}  actionlint-action_${manifest.version}.mjs\n`) {
		throw new Error('Action asset checksum differs');
	}
}

/** @param {unknown} value @param {CandidateManifest} manifest */
export function verifyRun(value, manifest) {
	const run = object(value, 'workflow run');
	const repository = object(run.repository, 'workflow repository');
	if (
		run.id !== manifest.run_id || run.run_attempt !== manifest.run_attempt
		|| run.event !== 'workflow_dispatch' || run.path !== workflowPath || run.head_branch !== 'master'
		|| run.head_sha !== manifest.source || repository.full_name !== manifest.repository
		|| run.status !== 'completed' || run.conclusion !== 'success'
	) throw new Error('Candidate does not match a successful release preparation run on master');
}

/** @param {string} root @param {string} endpoint @param {Record<string, unknown>} [body] @returns {unknown} */
function api(root, endpoint, body) {
	const args = ['api', endpoint];
	if (body !== undefined) args.push('--method', 'PATCH', '--input', '-');
	return JSON.parse(command('gh', args, root, body === undefined ? {} : { input: JSON.stringify(body) }));
}

/** @param {string} repository @param {string} tag @param {(args: string[]) => string} gh */
export function findRelease(repository, tag, gh) {
	// gh resolves pending draft tags through GraphQL; REST's tag lookup only finds published releases.
	const id = integer(
		Number(gh(['release', 'view', tag, '--repo', repository, '--json', 'databaseId', '--jq', '.databaseId'])),
		'release ID',
	);
	const release = object(JSON.parse(gh(['api', `repos/${repository}/releases/${id}`])), 'release');
	if (release.id !== id || release.tag_name !== tag) throw new Error('Release identity differs');
	return release;
}

/** @param {string} root @param {string} repository @param {{id: number}} asset @param {string} destination */
function downloadAsset(root, repository, asset, destination) {
	const fd = openSync(destination, 'wx');
	try {
		command(
			'gh',
			[
				'api',
				'-H',
				'Accept: application/octet-stream',
				`repos/${repository}/releases/assets/${integer(asset.id, 'release asset ID')}`,
			],
			root,
			{
				stdio: ['ignore', fd, 'pipe'],
			},
		);
	} finally {
		closeSync(fd);
	}
}

/** @param {GitCommand} runGit @param {string} head @param {CandidateManifest} manifest */
function isRecordedRelease(runGit, head, manifest) {
	return runGit('rev-list', '--parents', '-n', '1', head) === `${head} ${manifest.source} ${manifest.candidate}`
		&& runGit('rev-parse', `${head}^{tree}`) === runGit('rev-parse', `${manifest.source}^{tree}`);
}

/** @param {GitCommand} runGit @param {string} tag */
function localTag(runGit, tag) {
	return runGit('tag', '--list', tag) ? runGit('rev-parse', `refs/tags/${tag}`) : '';
}

/** @param {GitCommand} runGit @param {CandidateManifest} manifest */
function verifySignedTag(runGit, manifest) {
	const ref = `refs/tags/${manifest.tag}`;
	if (runGit('cat-file', '-t', ref) !== 'tag' || runGit('rev-parse', `${ref}^{commit}`) !== manifest.candidate) {
		throw new Error('Existing version tag does not annotate the validated candidate');
	}
	if (!runGit('cat-file', '-p', ref).includes('-----BEGIN PGP SIGNATURE-----')) {
		throw new Error('Version tag must use GPG signing');
	}
	runGit('verify-tag', ref);
	return runGit('rev-parse', ref);
}

/** @param {CandidateManifest} manifest @param {Record<string, unknown>} release @param {PromotionOperations} operations */
export function finalizePromotion(manifest, release, operations) {
	const runGit = operations.git;
	const releaseID = integer(release.id, 'release ID');
	const ref = `refs/tags/${manifest.tag}`;
	const remoteTag = runGit('ls-remote', 'origin', ref).split('\t')[0];
	if (remoteTag && !localTag(runGit, manifest.tag)) runGit('fetch', '--no-write-fetch-head', 'origin', `${ref}:${ref}`);
	let tagObject = localTag(runGit, manifest.tag);
	if (tagObject) tagObject = verifySignedTag(runGit, manifest);
	if (remoteTag && remoteTag !== tagObject) throw new Error('Remote version tag differs; it will not be replaced');
	if (!release.draft) {
		if (!remoteTag) throw new Error('Published release has no validated signed tag');
		return { tag: manifest.tag, releaseID, alreadyPublished: true };
	}
	if (release.target_commitish !== manifest.source) throw new Error('Draft target differs from prepared source');
	if (runGit('branch', '--show-current') !== 'master' || runGit('status', '--porcelain', '--untracked-files=all')) {
		throw new Error('Promotion requires a clean master checkout');
	}
	let head = runGit('rev-parse', 'HEAD');
	if (head !== manifest.source && !isRecordedRelease(runGit, head, manifest)) {
		throw new Error('Source advanced since preparation; prepare a new candidate or return to the prepared source');
	}
	const remoteHead = runGit('ls-remote', 'origin', 'refs/heads/master').split('\t')[0];
	commitValue(remoteHead);
	if (remoteHead !== manifest.source && remoteHead !== head) {
		runGit('fetch', '--no-tags', '--no-write-fetch-head', 'origin', 'refs/heads/master');
		if (!isRecordedRelease(runGit, remoteHead, manifest) || head !== manifest.source) {
			throw new Error('Remote master advanced since preparation');
		}
		runGit('merge', '--ff-only', remoteHead);
		head = remoteHead;
	}
	if (!tagObject) {
		runGit('-c', 'gpg.format=openpgp', 'tag', '-s', '-m', manifest.tag, manifest.tag, manifest.candidate);
		verifySignedTag(runGit, manifest);
	}
	if (head === manifest.source) {
		try {
			operations.record(manifest.tag, manifest.source);
		} catch (error) {
			throw new Error(
				`The validated signed tag exists locally. Inspect git status and finish any pending merge before retrying promotion.`,
				{ cause: error },
			);
		}
	}
	runGit('-c', 'push.followTags=false', 'push', '--atomic', 'origin', 'HEAD:refs/heads/master', `${ref}:${ref}`);
	try {
		const published = object(operations.publish(releaseID), 'published release');
		if (published.id !== releaseID || published.draft !== false || published.tag_name !== manifest.tag) {
			throw new Error('Publication response differs');
		}
	} catch (error) {
		throw new Error(
			`The signed tag was pushed. Rerun promotion with --version ${manifest.version} --run ${manifest.run_id} to verify or resume draft publication.`,
			{ cause: error },
		);
	}
	return { tag: manifest.tag, releaseID, alreadyPublished: false };
}

/** @param {string} root @param {string} version @param {number} runID @param {string} repository */
export async function promoteCandidate(root, version, runID, repository) {
	versionValue(version);
	integer(runID, 'workflow run ID');
	string(repository, /^[\w.-]+\/[\w.-]+$/, 'repository');
	const origin = git(root, 'remote', 'get-url', 'origin');
	const originRepository =
		/^(?:https:\/\/github\.com\/|git@github\.com:|ssh:\/\/git@github\.com\/)([\w.-]+\/[\w.-]+?)(?:\.git)?\/?$/.exec(
			origin,
		)?.[1];
	if (originRepository?.toLowerCase() !== repository.toLowerCase()) {
		throw new Error('origin does not match the candidate GitHub repository');
	}
	const run = object(api(root, `repos/${repository}/actions/runs/${runID}`), 'workflow run');
	const attempt = integer(run.run_attempt, 'workflow attempt');
	const tempRoot = resolve(tmpdir());
	const temporary = await mkdtemp(join(tempRoot, 'actionlint-promotion-'));
	try {
		const trusted = join(temporary, 'workflow');
		command('gh', [
			'run',
			'download',
			String(runID),
			'--repo',
			repository,
			'--name',
			`release-candidate-${runID}-${attempt}`,
			'--dir',
			trusted,
		], root);
		const trustedBytes = await readFile(join(trusted, manifestName));
		const manifest = parseManifest(JSON.parse(trustedBytes.toString('utf8')));
		if (manifest.version !== version || manifest.repository !== repository || manifest.run_id !== runID) {
			throw new Error('Requested release differs from candidate manifest');
		}
		verifyRun(run, manifest);
		const release = findRelease(repository, manifest.tag, (args) => command('gh', args, root));
		const releaseID = integer(release.id, 'release ID');
		if (release.tag_name !== manifest.tag || release.prerelease !== false || typeof release.draft !== 'boolean') {
			throw new Error('Release identity differs');
		}
		/** @type {unknown} */
		const listed = JSON.parse(
			command(
				'gh',
				['api', '--paginate', '--slurp', `repos/${repository}/releases/${releaseID}/assets?per_page=100`],
				root,
			),
		);
		if (!Array.isArray(listed) || !listed.every(Array.isArray)) throw new Error('Invalid release assets response');
		const assets = listed.flat().map((value) => {
			const asset = object(value, 'release asset');
			return { id: integer(asset.id, 'release asset ID'), name: assetName(asset.name) };
		});
		const expectedNames = [manifestName, ...manifest.assets.map((asset) => asset.name)].sort();
		if (JSON.stringify(assets.map((asset) => assetName(asset.name)).sort()) !== JSON.stringify(expectedNames)) {
			throw new Error('Draft asset set differs from validated candidate');
		}
		const directory = join(temporary, 'assets');
		await mkdir(directory);
		for (const asset of assets) downloadAsset(root, repository, asset, join(directory, asset.name));
		if (!(await readFile(join(directory, manifestName))).equals(trustedBytes)) {
			throw new Error('Draft manifest differs from immutable workflow artifact');
		}
		await verifyCandidate(root, directory, manifest);
		return finalizePromotion(manifest, release, {
			git: (...args) => git(root, ...args),
			record: (tag, source) => recordRelease(root, tag, source),
			publish: (id) =>
				api(root, `repos/${repository}/releases/${id}`, { draft: false, prerelease: false, make_latest: 'legacy' }),
		});
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	try {
		const [operation, ...args] = process.argv.slice(2);
		/** @type {Map<string, string>} */
		const options = new Map();
		for (let i = 0; i < args.length; i += 2) {
			if (
				!['--version', '--out-dir', '--directory', '--source', '--candidate', '--run', '--repo'].includes(args[i])
				|| !args[i + 1] || options.has(args[i])
			) throw new Error('Invalid or duplicate candidate option');
			options.set(args[i], args[i + 1]);
		}
		/** @param {string} name */
		const required = (name) => {
			const value = options.get(name);
			if (!value) throw new Error(`${name} is required`);
			return value;
		};
		if (operation === 'prepare') {
			const metadata = await prepareCandidate(sourceRoot, resolve(required('--out-dir')), required('--version'));
			if (process.env.GITHUB_OUTPUT) {
				await appendFile(
					process.env.GITHUB_OUTPUT,
					Object.entries(metadata).map(([key, value]) => `${key}=${value}\n`).join(''),
				);
			}
			console.log(JSON.stringify(metadata));
		} else if (operation === 'manifest') {
			await writeManifest(sourceRoot, resolve(required('--directory')), {
				version: required('--version'),
				source: required('--source'),
				candidate: required('--candidate'),
			});
		} else if (operation === 'verify') {
			const directory = resolve(required('--directory'));
			await verifyCandidate(
				sourceRoot,
				directory,
				parseManifest(JSON.parse(await readFile(join(directory, manifestName), 'utf8'))),
			);
		} else if (operation === 'promote') {
			const repository = options.get('--repo')
				|| command('gh', ['repo', 'view', '--json', 'nameWithOwner', '--jq', '.nameWithOwner'], sourceRoot);
			console.log(
				JSON.stringify(
					await promoteCandidate(sourceRoot, required('--version'), Number(required('--run')), repository),
				),
			);
		} else {
			throw new Error('Usage: release-candidate.mjs prepare|manifest|verify|promote [options]');
		}
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
