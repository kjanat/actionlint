import { execFileSync } from 'node:child_process';
import { appendFileSync, readFileSync, realpathSync } from 'node:fs';
import { resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

const optional = new Set(['nix', 'conformance', 'expression-probe']);

/** @param {NodeJS.ProcessEnv} environment @param {string} name */
function required(environment, name) {
	const value = environment[name];
	if (!value) throw new Error(`Missing ${name}`);
	return value;
}

/** Resolve runner-owned command files without allowing paths outside their directory. @param {NodeJS.ProcessEnv} environment @param {string} name */
function commandFile(environment, name) {
	const root = realpathSync(resolve(required(environment, 'RUNNER_TEMP'), '_runner_file_commands')) + sep;
	const file = realpathSync(required(environment, name));
	if (!file.startsWith(root)) throw new Error(`${name} must be inside the runner command directory`);
	return file;
}

/** Match the ordered *, ** and ! path patterns used by our workflows. @param {string} path @param {string[]} patterns */
export function matchesPaths(path, patterns) {
	let included = false;
	for (const pattern of patterns) {
		const exclude = pattern.startsWith('!');
		const glob = exclude ? pattern.slice(1) : pattern;
		// Reject unsupported CI path syntax.
		if (!glob || /[?+[\]{}\\]/.test(glob)) throw new Error('Unsupported CI path pattern');
		const expression = glob.split(/(\*\*\/|\*\*|\*)/).map(part => {
			if (part === '**/') return '(?:.*/)?';
			if (part === '**') return '.*';
			if (part === '*') return '[^/]*';
			return part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
		}).join('');
		if (new RegExp(`^${expression}$`).test(path)) included = !exclude;
	}
	return included;
}

/** @param {string} message @param {string} cwd */
export function ciTrailers(message, cwd = process.cwd()) {
	const parsed = execFileSync('git', ['interpret-trailers', '--parse'], { cwd, input: message, encoding: 'utf8' });
	const trailers = parsed.split('\n').map(line => {
		const colon = line.indexOf(':');
		return [line.slice(0, colon).trim().toLowerCase(), line.slice(colon + 1).trim().toLowerCase()];
	});
	if (trailers.some(([key, value]) => key === 'ci' && value === 'full')) return { full: true, skip: [] };
	const skip = trailers.filter(([key]) => key === 'ci-skip').flatMap(([, value]) =>
		value.split(/[\s,]+/).filter(Boolean)
	);
	if (skip.some(group => !optional.has(group))) throw new Error('CI-Skip only accepts optional test groups');
	if (trailers.some(([key, value]) => key === 'ci' && value !== 'full')) throw new Error('Unsupported CI trailer');
	return { full: false, skip };
}

/** @typedef {{shared: string[], jobs: Record<string, string[]>}} Filters */
/** @param {Filters} filters @param {string[]} paths @param {{full: boolean, skip: string[]}} trailers */
export function selectJobs(filters, paths, trailers = { full: false, skip: [] }) {
	const shared = paths.some(path => matchesPaths(path, filters.shared));
	return Object.fromEntries(
		Object.entries(filters.jobs).map(([job, patterns]) => [
			job,
			trailers.full || (!trailers.skip.includes(job) && (shared || paths.some(path => matchesPaths(path, patterns)))),
		]),
	);
}

/** @param {NodeJS.ProcessEnv} environment @param {string} cwd */
export function changedInputs(environment, cwd = process.cwd()) {
	const event = JSON.parse(readFileSync(required(environment, 'GITHUB_EVENT_PATH'), 'utf8'));
	const git = (/** @type {string[]} */ args) =>
		execFileSync('git', args, { cwd, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024, stdio: ['pipe', 'pipe', 'pipe'] });
	let base;
	let head;
	/** @type {ReturnType<typeof ciTrailers>} */
	let trailers = { full: false, skip: [] };
	if (environment.GITHUB_EVENT_NAME === 'pull_request') {
		base = event.pull_request.base.sha;
		head = event.pull_request.head.sha;
		if (!/^[a-f0-9]{40,64}$/.test(base) || !/^[a-f0-9]{40,64}$/.test(head)) throw new Error('Invalid comparison');
		base = git(['merge-base', base, head]).trim();
		trailers = ciTrailers(git(['log', '-1', '--format=%B', head]), cwd);
	} else if (environment.GITHUB_EVENT_NAME === 'push') {
		base = event.before;
		head = event.after;
		if (!/^[a-f0-9]{40,64}$/.test(base) || /^0+$/.test(base) || !/^[a-f0-9]{40,64}$/.test(head)) {
			throw new Error('No comparison for this push');
		}
	} else {
		return { paths: [], trailers: { full: true, skip: [] } };
	}
	const paths = git(['diff', '--name-only', '--no-renames', '-z', base, head, '--']).split('\0').filter(Boolean);
	return { paths, trailers };
}

/** @param {NodeJS.ProcessEnv} environment @param {string} cwd */
export function plan(environment = process.env, cwd = process.cwd()) {
	/** @type {Filters} */
	const filters = JSON.parse(required(environment, 'CI_FILTERS'));
	if (
		!filters.jobs || !Array.isArray(filters.shared)
		|| Object.entries(filters.jobs).some(([name, patterns]) =>
			!/^[a-z][a-z0-9-]*$/.test(name) || !Array.isArray(patterns)
		)
	) throw new Error('Invalid CI path filters');
	let jobs;
	let fallback = false;
	try {
		const { paths, trailers } = changedInputs(environment, cwd);
		jobs = selectJobs(filters, paths, trailers);
	} catch {
		// Missing history, invalid trailers or unsupported patterns must run more checks, never fewer.
		jobs = Object.fromEntries(Object.keys(filters.jobs).map(job => [job, true]));
		fallback = true;
	}
	const output = commandFile(environment, 'GITHUB_OUTPUT');
	const summary = environment.GITHUB_STEP_SUMMARY ? commandFile(environment, 'GITHUB_STEP_SUMMARY') : undefined;
	appendFileSync(output, `jobs=${JSON.stringify(jobs)}\n`);
	const rows = Object.entries(jobs).map(([job, run]) => `| ${job} | ${run ? 'Run' : 'Skip'} |`).join('\n');
	const report = `${
		fallback ? 'Comparison or trailer unavailable; running every check.\n\n' : ''
	}| Check | Selection |\n| --- | --- |\n${rows}\n`;
	if (summary) appendFileSync(summary, report);
	return jobs;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) plan();
