import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { appendFileSync, chmodSync, copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/** @typedef {'auto' | 'require'} VerifyMode */
/** @typedef {{version: string, platform: string, installer: string, source: string, verify: VerifyMode}} Identity */
/** @typedef {Identity & {runner: string, run: string}} Receipt */

const actionPath = dirname(fileURLToPath(import.meta.url));

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
	const metadata = readFileSync(join(actionPath, 'action.yml'), 'utf8');
	const installer = metadata.match(/uses: (kjanat\/runner@[0-9a-f]{40})\b/)?.[1];
	if (!installer) throw new Error('setup-runner must pin its upstream installer');
	const source = createHash('sha256').update(metadata).update(readFileSync(fileURLToPath(import.meta.url))).digest(
		'hex',
	);
	return { version, platform, verify, installer, source };
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
		return receipt.runner === hash(join(directory, runner)) && receipt.run === hash(join(directory, run));
	} catch {
		return false;
	}
}

/** @param {string} installed @param {string} directory @param {Identity} expected @param {string} installedVersion */
export function seal(installed, directory, expected, installedVersion) {
	if (installedVersion !== expected.version) throw new Error('runner installer returned a different version');
	mkdirSync(directory, { recursive: true });
	const [runner, run] = binaries(expected);
	for (const binary of [runner, run]) {
		copyFileSync(join(installed, binary), join(directory, binary));
		if (!expected.platform.startsWith('win32-')) chmodSync(join(directory, binary), 0o755);
	}
	/** @type {Receipt} */
	const receipt = { ...expected, runner: hash(join(directory, runner)), run: hash(join(directory, run)) };
	writeFileSync(join(directory, 'receipt.json'), JSON.stringify(receipt));
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

function main() {
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
		case 'seal':
			seal(required('INSTALLED_BIN_DIR'), directory, expected, required('INSTALLED_VERSION'));
			break;
		case 'publish':
			for (const [key, value] of Object.entries(publish(directory, expected))) output(key, value);
			appendFileSync(required('GITHUB_PATH'), `${directory}\n`);
			break;
		default:
			throw new Error('unknown runner setup phase');
	}
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
