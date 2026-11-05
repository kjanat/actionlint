import { spawn } from 'node:child_process';
import { readFile, realpath } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';

import { findNix } from '#release/nix-command.mjs';
import { releaseNotes } from '#release/release-candidate.mjs';
import {
	changelogReleaseNotes,
	prepareChangelog,
	unreleasedEntries,
	validateChangelog,
} from '#release/version-changelog.mjs';
import { checkReferences, parseVersion, planVersionUpdates, writeUpdates } from '#release/version-references.mjs';
import { targets } from '#release/version-targets.mjs';

/** @typedef {import('#release/nix-command.mjs').ExecuteCommand} ExecuteCommand */
/** @typedef {{root: string, version?: string, check: boolean, commit: boolean, push: boolean, nixProgram?: string, nixArguments: string[]}} VersionOptions */

/** @param {string} root @param {AbortSignal} signal @param {NodeJS.ProcessEnv} [env] @returns {ExecuteCommand} */
export function commandExecutor(root, signal, env = process.env) {
	return (program, args, options = {}) =>
		new Promise((resolve, reject) => {
			const child = spawn(program, args, {
				cwd: root,
				signal,
				env,
				timeout: options.timeout,
				killSignal: 'SIGKILL',
				stdio: options.live ? ['ignore', 'inherit', 'inherit'] : ['ignore', 'pipe', 'pipe'],
				windowsHide: true,
			});
			/** @type {Buffer[]} */
			const stdout = [];
			/** @type {Buffer[]} */
			const stderr = [];
			/** @type {Error | undefined} */
			let failure;
			child.stdout?.on('data', (/** @type {Buffer} */ chunk) => stdout.push(chunk));
			child.stderr?.on('data', (/** @type {Buffer} */ chunk) => stderr.push(chunk));
			child.on('error', error => {
				failure = error;
			});
			child.once('close', (code, stoppedBy) => {
				if (signal.aborted) {
					reject(signal.reason);
					return;
				}
				if (failure || code !== 0) {
					const detail = Buffer.concat(stderr).toString('utf8').trim()
						|| Buffer.concat(stdout).toString('utf8').trim()
						|| failure?.message
						|| `exit status ${code ?? stoppedBy}`;
					reject(new Error(`${program} ${args.join(' ')} failed: ${detail}`, { cause: failure }));
					return;
				}
				resolve(Buffer.concat(stdout));
			});
		});
}

/** @param {string} root @param {string} tag @param {boolean} commit @param {boolean} push @param {ExecuteCommand} execute */
export async function checkRepository(root, tag, commit, push, execute) {
	/** @param {...string} args */
	const git = async (...args) => (await execute('git', args)).toString('utf8').trim();
	if (await realpath(await git('rev-parse', '--show-toplevel')) !== await realpath(root)) {
		throw new Error('Run version preparation at the repository root');
	}
	if ((commit || push) && await git('status', '--porcelain', '--untracked-files=all')) {
		throw new Error(
			'Automatic release commits require a clean working tree; omit --commit and --push to prepare edits for review',
		);
	}
	const branch = await git('rev-parse', '--abbrev-ref', 'HEAD');
	if (branch === 'HEAD') throw new Error('Version preparation requires a branch, not a detached HEAD');
	if (push && branch !== 'master') {
		throw new Error(`--push requires master; the current branch is ${JSON.stringify(branch)}`);
	}
	if (await git('tag', '--list', tag)) throw new Error(`Tag ${tag} already exists`);
	if (await git('ls-remote', '--tags', 'origin', `refs/tags/${tag}`)) {
		throw new Error(`Tag ${tag} already exists on origin`);
	}
}

/** @param {VersionOptions} options @param {{execute?: ExecuteCommand, report?: (line: string) => void, signal?: AbortSignal, now?: Date, platform?: string}} [environment] */
export async function prepareVersion(options, environment = {}) {
	if (options.check && (options.version || options.commit || options.push)) {
		throw new Error('--check does not accept a version, --commit or --push');
	}
	if (!options.check && !options.version) throw new Error('Specify one stable release version, such as 2.0.0');
	if (options.version) parseVersion(options.version);
	const root = resolve(options.root);
	const signal = environment.signal ?? new AbortController().signal;
	const execute = environment.execute ?? commandExecutor(root, signal);
	const report = environment.report ?? console.log;
	const changelog = await readFile(join(root, 'CHANGELOG.md'), 'utf8');
	if (options.check) {
		const version = await checkReferences(root, targets, report);
		unreleasedEntries(changelog);
		validateChangelog(changelog);
		await releaseNotes(root, `v${version}`);
		report(`docs/releases/v${version}.md describes v${version}`);
		return { version, committed: false, pushed: false };
	}
	if (!options.version) throw new Error('Specify one stable release version, such as 2.0.0');
	const version = options.version;
	const tag = `v${version}`;
	await checkRepository(root, tag, options.commit, options.push, execute);
	changelogReleaseNotes(changelog, tag);
	await releaseNotes(root, tag);
	const date = new Intl.DateTimeFormat('en-CA', {
		timeZone: 'Europe/Amsterdam',
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
	}).format(environment.now ?? new Date());
	const updates = await planVersionUpdates(root, targets, version);
	updates.push({ path: 'CHANGELOG.md', original: changelog, content: prepareChangelog(changelog, tag, date) });
	const nix = await findNix({
		program: options.nixProgram,
		args: options.nixArguments,
		platform: environment.platform ?? process.platform,
	}, execute);
	signal.throwIfAborted();
	await writeUpdates(root, updates);
	await checkReferences(root, targets);
	for (const update of updates) report(`${update.path}: verified for ${tag}`);
	try {
		await execute(nix.program, [...nix.args, 'flake', 'check', '--no-update-lock-file', '--print-build-logs'], {
			live: true,
		});
	} catch (error) {
		throw new Error('Nix verification failed. The edits are available for review; no commit or tag was created.', {
			cause: error,
		});
	}
	if (!options.commit && !options.push) {
		report(
			`Review the edits, then commit them and prepare the draft with: gh workflow run release-prepare.yml --ref master -f version=${version}`,
		);
		return { version, committed: false, pushed: false };
	}
	await execute('git', ['add', '--', ...updates.map(update => update.path)], { live: true });
	await execute('git', ['commit', '-m', `chore(release): prepare ${tag}`], { live: true });
	if (!options.push) {
		report(
			`The source commit is local. After merging it into master, run: gh workflow run release-prepare.yml --ref master -f version=${version}`,
		);
		return { version, committed: true, pushed: false };
	}
	await execute('git', ['push', 'origin', 'master'], { live: true });
	const source = (await execute('git', ['rev-parse', 'HEAD'])).toString('utf8').trim();
	const args = [
		'workflow',
		'run',
		'release-prepare.yml',
		'--ref',
		'master',
		'-f',
		`version=${version}`,
		'-f',
		`source=${source}`,
	];
	try {
		await execute('gh', args, { live: true });
	} catch (error) {
		throw new Error(
			`The source commit was pushed, but draft preparation was not dispatched. Retry: gh ${args.join(' ')}`,
			{ cause: error },
		);
	}
	report(
		`After draft preparation succeeds, promote it with: node tools/release/release-candidate.mjs promote --version ${version} --run RUN_ID`,
	);
	return { version, committed: true, pushed: true };
}

/** @param {string[]} args @param {AbortSignal} signal */
export async function main(args, signal) {
	const { values, positionals } = parseArgs({
		args,
		allowPositionals: true,
		options: {
			check: { type: 'boolean', default: false },
			commit: { type: 'boolean', default: false },
			push: { type: 'boolean', default: false },
			root: { type: 'string', default: '.' },
			'nix-program': { type: 'string' },
			'nix-arg': { type: 'string', multiple: true, default: [] },
			help: { type: 'boolean', short: 'h', default: false },
		},
	});
	if (values.help) {
		console.log(`Usage: node tools/release/prepare-version.mjs [options] [VERSION]

  --check                 Check version references and release notes without edits
  --commit                Create the source commit after Nix verification
  --push                  Commit, push master and dispatch draft preparation
  --root PATH             Repository root (default: current directory)
  --nix-program PROGRAM   Select a Nix executable or launcher
  --nix-arg ARG           Add one launcher argument (repeatable)
  -h, --help              Show help

Without --commit or --push, review the prepared edits in the working tree.
No version tag or release is published by this command.
Example: node tools/release/prepare-version.mjs 2.0.0
Example: node tools/release/prepare-version.mjs --check`);
		return;
	}
	if (positionals.length > 1) throw new Error('Specify only one release version');
	await prepareVersion({
		root: values.root,
		version: positionals[0],
		check: values.check,
		commit: values.commit,
		push: values.push,
		nixProgram: values['nix-program'],
		nixArguments: values['nix-arg'],
	}, { signal });
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
	const controller = new AbortController();
	const abort = () => controller.abort();
	process.on('SIGINT', abort);
	process.on('SIGTERM', abort);
	try {
		await main(process.argv.slice(2), controller.signal);
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	} finally {
		process.off('SIGINT', abort);
		process.off('SIGTERM', abort);
	}
}
