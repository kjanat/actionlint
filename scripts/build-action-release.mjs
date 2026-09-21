// Prepare release files outside the checkout. --commit uses an isolated Git
// index: it creates a child of the signed source commit without changing HEAD,
// the working tree, the user's index, or any tag.
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readdir, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const sourceRoot = fileURLToPath(new URL('../', import.meta.url));
const releaseFiles = ['action.yml', 'README.md', 'LICENSE.txt', 'dist/main.mjs', 'SHA256SUMS'];

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

/** @param {string} root @param {string} directory @param {string} version */
export async function prepareRelease(root, directory, version) {
	assertVersion(version);
	assertOutsideCheckout(root, directory);
	assertOutsideCheckout(await realpath(root), await resolvedDestination(directory));
	await mkdir(directory, { recursive: true });
	if ((await readdir(directory)).length > 0) throw new Error('Release output directory must be empty');
	const action = join(directory, 'action');
	const assets = join(directory, 'assets');
	await mkdir(assets);
	execute(
		process.execPath,
		['--run', 'build', '--', '--out-dir', join(action, 'dist')],
		join(root, 'packages/github-action'),
		{ ...process.env, ACTIONLINT_VERSION: version },
	);
	const sourceMetadata = await readFile(join(root, 'action.yml'), 'utf8');
	const main = /\bmain:\s+dist\/main\.mjs(?=\s*(?:[,}]|$))/gm;
	if ([...sourceMetadata.matchAll(main)].length !== 1) {
		throw new Error('Expected the action metadata to run dist/main.mjs');
	}
	await copyFile(join(root, 'action.yml'), join(action, 'action.yml'));
	for (const name of ['README.md', 'LICENSE.txt']) await copyFile(join(root, name), join(action, name));
	const bundle = await readFile(join(action, 'dist/main.mjs'));
	const digest = createHash('sha256').update(bundle).digest('hex');
	const asset = `actionlint-action_${version}.mjs`;
	await copyFile(join(action, 'dist/main.mjs'), join(assets, asset));
	await writeFile(join(assets, `actionlint-action_${version}_checksums.txt`), `${digest}  ${asset}\n`);
}

/** @param {string} root @param {string} directory @param {string} version @param {string} parent */
export async function createReleaseCommit(root, directory, version, parent) {
	assertVersion(version);
	assertOutsideCheckout(root, directory);
	assertOutsideCheckout(await realpath(root), await realpath(directory));
	if (parent.length !== 40 || !/^[a-f0-9]+$/.test(parent)) throw new Error('--parent must be a full source commit SHA');
	execute('git', ['cat-file', '-e', `${parent}^{commit}`], root);
	const action = join(directory, 'action');
	const digest = createHash('sha256').update(await readFile(join(action, 'dist/main.mjs'))).digest('hex');
	if (await readFile(join(action, 'SHA256SUMS'), 'utf8') !== `${digest}  dist/main.mjs\n`) {
		throw new Error('Release bundle checksum does not match');
	}
	const tempRoot = resolve(tmpdir());
	const temporary = await mkdtemp(join(tempRoot, 'actionlint-release-index-'));
	try {
		const env = { ...process.env, GIT_INDEX_FILE: join(temporary, 'index') };
		execute('git', ['read-tree', '--empty'], root, env);
		for (const name of releaseFiles) {
			const blob = execute('git', ['hash-object', '-w', '--no-filters', '--', join(action, name)], root, env);
			execute('git', ['update-index', '--add', '--cacheinfo', `100644,${blob},${name}`], root, env);
		}
		const tree = execute('git', ['write-tree'], root, env);
		return execute('git', ['commit-tree', tree, '-p', parent, '-m', `Build GitHub Action for v${version}`], root, env);
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	try {
		/** @type {Map<string, string>} */
		const options = new Map();
		let commit = false;
		const args = process.argv.slice(2);
		for (let i = 0; i < args.length; i++) {
			if (args[i] === '--commit') {
				commit = true;
				continue;
			}
			if (!['--out-dir', '--version', '--parent'].includes(args[i]) || !args[i + 1]) {
				throw new Error(
					'Usage: build-action-release.mjs --out-dir ABSOLUTE_DIR --version VERSION [--commit --parent SHA]',
				);
			}
			options.set(args[i], args[++i]);
		}
		const directory = options.get('--out-dir');
		const version = options.get('--version');
		if (!directory || !version) throw new Error('--out-dir and --version are required');
		if (commit) {
			const parent = options.get('--parent');
			if (!parent) throw new Error('--parent is required with --commit');
			console.log(await createReleaseCommit(sourceRoot, directory, version, parent));
		} else {
			await prepareRelease(sourceRoot, directory, version);
		}
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
