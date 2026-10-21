import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import {
	appendFileSync,
	chmodSync,
	copyFileSync,
	lstatSync,
	mkdirSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { release } from './proofs.mjs';

/** @typedef {'auto' | 'require'} VerifyMode */
/** @typedef {{version: string, platform: string, installer: string, source: string, verify: VerifyMode, runnerSHA256: string, runSHA256: string}} Identity */
/** @typedef {Identity & {runner: string, run: string}} Receipt */
/** @typedef {(file: string, args: string[], options: {cwd: string, env: NodeJS.ProcessEnv, encoding: 'utf8', timeout: number, maxBuffer: number}) => string} Execute */

const actionPath = dirname(fileURLToPath(import.meta.url));
const proofPath = join(actionPath, 'proofs', release.version);

/** @param {string} version @param {string} platform */
export function assetFor(version, platform) {
	if (version !== release.version || !Object.hasOwn(release.assets, platform)) {
		throw new Error(`runner ${version}/${platform} has no reviewed offline provenance; update setup-runner proofs`);
	}
	const asset = release.assets[platform];
	if (!asset) throw new Error('missing runner asset proof');
	return asset;
}

/** @param {string} path */
export function pinnedVersion(path) {
	for (const line of readFileSync(path, 'utf8').split(/\r?\n/)) {
		const [rawTool, rawVersion] = line.replace(/#.*/, '').trim().split(/\s+/);
		if (!rawTool || !rawVersion) continue;
		const tool = rawTool.replace(/\[[^\]]*\]$/, '').toLowerCase();
		const name = tool.includes(':') ? tool.slice(tool.indexOf(':') + 1) : tool;
		if (!['runner', 'runner-run', 'kjanat/runner'].includes(name)) continue;
		const version = rawVersion.replace(/^v/, '');
		if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
			throw new Error('CI runner needs an exact version pin in version-file');
		}
		return version;
	}
	throw new Error('version-file has no runner version pin');
}

/** @param {string} mode @returns {VerifyMode} */
export function verificationMode(mode) {
	if (mode !== 'auto' && mode !== 'require') throw new Error('runner verify must be auto or require');
	return mode;
}

/** @param {string} version @param {VerifyMode} verify @param {string} platform @returns {Identity} */
export function identity(version, verify, platform) {
	const asset = assetFor(version, platform);
	const metadata = readFileSync(join(actionPath, 'action.yml'), 'utf8');
	const installer = `${release.repository}@${release.sourceDigest}`;
	const source = createHash('sha256')
		.update(metadata)
		.update(readFileSync(fileURLToPath(import.meta.url)))
		.update(readFileSync(join(actionPath, 'proofs.mjs')))
		.update(readFileSync(join(proofPath, asset.bundle)))
		.update(readFileSync(join(proofPath, 'trusted-root.jsonl')))
		.digest('hex');
	return { version, platform, verify, installer, source, runnerSHA256: asset.runnerSHA256, runSHA256: asset.runSHA256 };
}

/** @param {Identity} expected */
export function cacheKey(expected) {
	return `actionlint-runner-${createHash('sha256').update(JSON.stringify(expected)).digest('hex')}`;
}

/** @param {string} file */
const hash = file => createHash('sha256').update(readFileSync(file)).digest('hex');
/** @param {Identity} expected */
const binaries = expected => expected.platform.startsWith('win32-') ? ['runner.exe', 'run.exe'] : ['runner', 'run'];

/** @param {string} directory @param {Identity} expected */
export function inspect(directory, expected) {
	try {
		/** @type {unknown} */
		const receipt = JSON.parse(readFileSync(join(directory, 'receipt.json'), 'utf8'));
		if (typeof receipt !== 'object' || receipt === null || Array.isArray(receipt)) return false;
		if (
			!('version' in receipt) || receipt.version !== expected.version
			|| !('platform' in receipt) || receipt.platform !== expected.platform
			|| !('installer' in receipt) || receipt.installer !== expected.installer
			|| !('source' in receipt) || receipt.source !== expected.source
			|| !('verify' in receipt) || receipt.verify !== expected.verify
			|| !('runner' in receipt) || typeof receipt.runner !== 'string'
			|| !('run' in receipt) || typeof receipt.run !== 'string'
		) return false;
		const [runner, run] = binaries(expected);
		return receipt.runner === expected.runnerSHA256 && receipt.run === expected.runSHA256
			&& expected.runnerSHA256 === hash(join(directory, runner)) && expected.runSHA256 === hash(join(directory, run));
	} catch {
		return false;
	}
}

/** @param {string} installed @param {string} directory @param {Identity} expected @param {string} installedVersion */
export function seal(installed, directory, expected, installedVersion) {
	if (installedVersion !== expected.version) throw new Error('runner installer returned a different version');
	const entry = lstatSync(directory, { throwIfNoEntry: false });
	if (entry && (!entry.isDirectory() || entry.isSymbolicLink())) throw new Error('runner cache must be a directory');
	mkdirSync(directory, { recursive: true });
	const [runner, run] = binaries(expected);
	for (const binary of [runner, run]) {
		const entry = lstatSync(join(directory, binary), { throwIfNoEntry: false });
		if (entry && (!entry.isFile() || entry.isSymbolicLink())) throw new Error('runner cache binary must be a file');
		copyFileSync(join(installed, binary), join(directory, binary));
		if (!expected.platform.startsWith('win32-')) chmodSync(join(directory, binary), 0o755);
	}
	/** @type {Receipt} */
	const receipt = { ...expected, runner: hash(join(directory, runner)), run: hash(join(directory, run)) };
	if (receipt.runner !== expected.runnerSHA256 || receipt.run !== expected.runSHA256) {
		throw new Error('runner binaries do not match the reviewed release');
	}
	writeFileSync(join(directory, 'receipt.json'), JSON.stringify(receipt));
}

/** @param {import('./proofs.mjs').Asset} asset @param {typeof fetch} request */
async function download(asset, request) {
	const response = await request(asset.url, { signal: AbortSignal.timeout(60_000), redirect: 'error' });
	if (!response.ok || !response.body) throw new Error(`runner archive download failed: HTTP ${response.status}`);
	const reader = response.body.getReader();
	/** @type {Uint8Array[]} */
	const chunks = [];
	let size = 0;
	try {
		for (;;) {
			const chunk = await reader.read();
			if (chunk.done) break;
			size += chunk.value.byteLength;
			if (size > asset.bytes) {
				await reader.cancel();
				throw new Error('runner archive exceeds its reviewed size');
			}
			chunks.push(chunk.value);
		}
	} finally {
		reader.releaseLock();
	}
	const bytes = Buffer.concat(chunks);
	if (bytes.length !== asset.bytes || createHash('sha256').update(bytes).digest('hex') !== asset.sha256) {
		throw new Error('runner archive SHA256 or size mismatch');
	}
	if (`sha512-${createHash('sha512').update(bytes).digest('base64')}` !== asset.integrity) {
		throw new Error('runner archive npm integrity mismatch');
	}
	return bytes;
}

/** @param {string} directory @param {Identity} expected @param {{request?: typeof fetch, execute?: Execute, proofs?: string}} [dependencies] */
export async function install(directory, expected, dependencies = {}) {
	if (inspect(directory, expected)) return;
	const asset = assetFor(expected.version, expected.platform);
	const request = dependencies.request ?? globalThis.fetch;
	const execute = dependencies.execute ?? ((file, args, options) => execFileSync(file, args, options));
	const proofs = dependencies.proofs ?? proofPath;
	mkdirSync(dirname(directory), { recursive: true });
	const staging = mkdtempSync(join(dirname(directory), '.runner-install-'));
	try {
		const archive = join(staging, 'archive.tgz');
		writeFileSync(archive, await download(asset, request));
		const config = join(staging, 'gh-config');
		mkdirSync(config);
		/** @type {Parameters<Execute>[2]} */
		const options = {
			cwd: staging,
			encoding: 'utf8',
			timeout: 60_000,
			maxBuffer: 2 * 1024 * 1024,
			env: {
				...process.env,
				GH_CONFIG_DIR: config,
				GH_TOKEN: '',
				GITHUB_TOKEN: '',
				GH_ENTERPRISE_TOKEN: '',
				GITHUB_ENTERPRISE_TOKEN: '',
				GH_HOST: 'github.com',
				GH_DEBUG: '',
				HTTP_PROXY: 'http://127.0.0.1:1',
				HTTPS_PROXY: 'http://127.0.0.1:1',
				ALL_PROXY: 'http://127.0.0.1:1',
				NO_PROXY: '',
			},
		};
		execute('gh', [
			'attestation',
			'verify',
			archive,
			'--bundle',
			join(proofs, asset.bundle),
			'--custom-trusted-root',
			join(proofs, 'trusted-root.jsonl'),
			'--repo',
			release.repository,
			'--source-ref',
			release.sourceRef,
			'--source-digest',
			release.sourceDigest,
			'--signer-workflow',
			release.signerWorkflow,
			'--signer-digest',
			release.sourceDigest,
			'--predicate-type',
			release.predicateType,
			'--deny-self-hosted-runners',
		], options);
		const installed = join(staging, 'installed');
		mkdirSync(installed);
		const [runner, run] = binaries(expected);
		execute('tar', [
			'-xzf',
			'archive.tgz',
			'-C',
			'installed',
			'--strip-components=2',
			`package/bin/${runner}`,
			`package/bin/${run}`,
		], options);
		seal(installed, directory, expected, expected.version);
	} finally {
		rmSync(staging, { recursive: true, force: true });
	}
}

/** @param {string} directory @param {Identity} expected @param {(file: string, args: string[]) => string} [execute] */
export function publish(directory, expected, execute = (file, args) => execFileSync(file, args, { encoding: 'utf8' })) {
	if (!inspect(directory, expected)) throw new Error('runner cache failed identity or binary verification');
	const [runner, run] = binaries(expected);
	const runnerPath = join(directory, runner);
	const version = execute(runnerPath, ['--version']).match(
		/\b(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)\b/,
	)?.[1];
	if (version !== expected.version) throw new Error('cached runner reports a different version');
	return { version, 'bin-dir': directory, 'runner-bin': runnerPath, 'run-bin': join(directory, run) };
}

/** @param {string} key @param {string} value */
function output(key, value) {
	if (/[\r\n]/.test(value)) throw new Error('runner setup output contains a newline');
	appendFileSync(required('GITHUB_OUTPUT'), `${key}=${value}\n`);
}

/** @param {string} key */
function required(key) {
	const value = process.env[key];
	if (!value) throw new Error(`${key} is required`);
	return value;
}

async function main() {
	const version = pinnedVersion(resolve(process.env.GITHUB_WORKSPACE || process.cwd(), required('VERSION_FILE')));
	const verify = verificationMode(required('VERIFY_MODE'));
	let platform = `${process.platform}-${process.arch}`;
	if (process.platform === 'linux') {
		const report = process.report.getReport();
		if (
			typeof report !== 'object' || report === null || !('header' in report)
			|| typeof report.header !== 'object' || report.header === null
		) throw new Error('could not determine runner libc');
		platform += 'glibcVersionRuntime' in report.header && report.header.glibcVersionRuntime ? '-gnu' : '-musl';
	}
	const expected = identity(version, verify, platform);
	const directory = join(required('RUNNER_TEMP'), 'actionlint-runner', cacheKey(expected));
	switch (process.argv[2]) {
		case 'prepare':
			output('version', version);
			output('cache-dir', directory);
			output('cache-key', cacheKey(expected));
			break;
		case 'inspect':
			output('hit', String(inspect(directory, expected)));
			break;
		case 'install':
			await install(directory, expected);
			break;
		case 'publish':
			for (const [key, value] of Object.entries(publish(directory, expected))) output(key, value);
			appendFileSync(required('GITHUB_PATH'), `${directory}\n`);
			break;
		default:
			throw new Error('unknown runner setup phase');
	}
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await main();
