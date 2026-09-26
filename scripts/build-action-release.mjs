// Prepare release files outside the checkout. --commit uses an isolated Git
// index: it adds the bundle to the source tree without changing HEAD,
// the working tree, the user's index, or any tag.
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readdir, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const sourceRoot = fileURLToPath(new URL('../', import.meta.url));
const generatedFiles = ['action.mjs', 'SHA256SUMS'];

/** @param {string} command @param {string[]} args @param {string} cwd @param {NodeJS.ProcessEnv} [env] */
function execute(command, args, cwd, env = process.env) {
	const result = spawnSync(command, args, { cwd, env, encoding: 'utf8' });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${command} failed (${result.status}): ${result.stderr || result.stdout}`);
	return result.stdout.trim();
}

/** @param {string} root @param {string} directory */
export function assertOutsideCheckout(root, directory) {
	if (!isAbsolute(directory)) throw new Error('--out-dir must be absolute');
	const rel = relative(resolve(root), resolve(directory));
	if (
		rel === ''
		|| (rel !== '..' && !rel.startsWith(`..${process.platform === 'win32' ? '\\' : '/'}`) && !isAbsolute(rel))
	) {
		throw new Error('Release output must be outside the source checkout');
	}
}

/** @param {string} directory @returns {Promise<string>} */
async function resolvedDestination(directory) {
	try {
		return await realpath(directory);
	} catch (error) {
		if (!(error instanceof Error) || !('code' in error) || error.code !== 'ENOENT') throw error;
		const parent = dirname(directory);
		if (parent === directory) throw error;
		return join(await resolvedDestination(parent), relative(parent, directory));
	}
}

/** @param {string} version */
function assertVersion(version) {
	if (version.trim() !== version || !/^\d+\.\d+\.\d+$/.test(version)) {
		throw new Error('Version must have the form MAJOR.MINOR.PATCH');
	}
}

/** @param {string} root @param {string} directory @param {string} version @param {boolean} [fromTag] */
export async function prepareRelease(root, directory, version, fromTag = false) {
	assertVersion(version);
	assertOutsideCheckout(root, directory);
	assertOutsideCheckout(await realpath(root), await resolvedDestination(directory));
	await mkdir(directory, { recursive: true });
	if ((await readdir(directory)).length > 0) throw new Error('Release output directory must be empty');
	const action = join(directory, 'action');
	const assets = join(directory, 'assets');
	await mkdir(assets);
	if (fromTag) {
		await verifyBundle(root);
		await mkdir(action);
		for (const name of generatedFiles) await copyFile(join(root, name), join(action, name));
	} else {
		execute(
			process.execPath,
			['--run', 'build', '--', '--out-dir', action],
			join(root, 'packages/github-action'),
			{ ...process.env, ACTIONLINT_VERSION: version },
		);
	}
	const sourceMetadata = await readFile(join(root, 'action.yml'), 'utf8');
	const main = /\bmain:\s+action\.mjs(?=\s*(?:[,}]|$))/gm;
	if ([...sourceMetadata.matchAll(main)].length !== 1) {
		throw new Error('Expected the action metadata to run action.mjs');
	}
	await copyFile(join(root, 'action.yml'), join(action, 'action.yml'));
	for (const name of ['README.md', 'LICENSE.txt']) await copyFile(join(root, name), join(action, name));
	const bundle = await readFile(join(action, 'action.mjs'));
	const digest = createHash('sha256').update(bundle).digest('hex');
	const asset = `actionlint-action_${version}.mjs`;
	await copyFile(join(action, 'action.mjs'), join(assets, asset));
	await writeFile(join(assets, `actionlint-action_${version}_checksums.txt`), `${digest}  ${asset}\n`);
}

/** @param {string} directory */
async function verifyBundle(directory) {
	const digest = createHash('sha256').update(await readFile(join(directory, 'action.mjs'))).digest('hex');
	if (await readFile(join(directory, 'SHA256SUMS'), 'utf8') !== `${digest}  action.mjs\n`) {
		throw new Error('Release bundle checksum does not match');
	}
}

/** @param {string} root @param {string} directory @param {string} version @param {string} parent */
export async function createReleaseCommit(root, directory, version, parent) {
	assertVersion(version);
	assertOutsideCheckout(root, directory);
	assertOutsideCheckout(await realpath(root), await realpath(directory));
	if (parent.length !== 40 || !/^[a-f0-9]+$/.test(parent)) throw new Error('--parent must be a full source commit SHA');
	execute('git', ['cat-file', '-e', `${parent}^{commit}`], root);
	const action = join(directory, 'action');
	await verifyBundle(action);
	for (const name of ['action.yml', 'README.md', 'LICENSE.txt']) {
		const original = execute('git', ['show', `${parent}:${name}`], root).replaceAll('\r\n', '\n');
		const prepared = (await readFile(join(action, name), 'utf8')).trim().replaceAll('\r\n', '\n');
		if (original !== prepared) throw new Error(`Prepared ${name} differs from the source commit`);
	}
	const tempRoot = resolve(tmpdir());
	const temporary = await mkdtemp(join(tempRoot, 'actionlint-release-index-'));
	try {
		const env = { ...process.env, GIT_INDEX_FILE: join(temporary, 'index') };
		execute('git', ['read-tree', parent], root, env);
		for (const name of generatedFiles) {
			const blob = execute('git', ['hash-object', '-w', '--no-filters', '--', join(action, name)], root, env);
			execute('git', ['update-index', '--add', '--cacheinfo', `100644,${blob},${name}`], root, env);
		}
		const tree = execute('git', ['write-tree'], root, env);
		return execute('git', ['commit-tree', tree, '-p', parent, '-m', `Build GitHub Action for v${version}`], root, env);
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
}

/** @param {string} root @param {string} tag @param {string} parent */
export function recordRelease(root, tag, parent) {
	if (execute('git', ['rev-parse', 'HEAD'], root) !== parent) {
		throw new Error('Source HEAD changed during release preparation');
	}
	if (execute('git', ['status', '--porcelain', '--untracked-files=all'], root)) {
		throw new Error('Source checkout changed during release preparation');
	}
	// Retain the source tree while making the bundled tag visible to git describe.
	execute('git', ['merge', '--no-ff', '--strategy=ours', '-m', `Record bundled release ${tag}`, tag], root);
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	try {
		/** @type {Map<string, string>} */
		const options = new Map();
		let commit = false;
		let fromTag = false;
		let tag = false;
		const args = process.argv.slice(2);
		for (let i = 0; i < args.length; i++) {
			if (args[i] === '--commit') {
				commit = true;
				continue;
			}
			if (args[i] === '--from-tag') {
				fromTag = true;
				continue;
			}
			if (args[i] === '--tag') {
				tag = true;
				continue;
			}
			if (!['--out-dir', '--version', '--parent'].includes(args[i]) || !args[i + 1]) {
				throw new Error(
					'Usage: build-action-release.mjs --version VERSION (--tag | --out-dir ABSOLUTE_DIR [--from-tag | --commit --parent SHA])',
				);
			}
			options.set(args[i], args[++i]);
		}
		const directory = options.get('--out-dir');
		const version = options.get('--version');
		if (!version) throw new Error('--version is required');
		if ([commit, fromTag, tag].filter(Boolean).length > 1) {
			throw new Error('--tag, --commit and --from-tag cannot be combined');
		}
		if (tag) {
			throw new Error(
				`Prepare and validate a draft first, then run: node scripts/release-candidate.mjs promote --version ${version} --run RUN_ID`,
			);
		} else if (!directory) {
			throw new Error('--out-dir is required');
		} else if (commit) {
			const parent = options.get('--parent');
			if (!parent) throw new Error('--parent is required with --commit');
			console.log(await createReleaseCommit(sourceRoot, directory, version, parent));
		} else {
			await prepareRelease(sourceRoot, directory, version, fromTag);
		}
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
