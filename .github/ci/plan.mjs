import { execFileSync } from 'node:child_process';
import { appendFileSync, readFileSync, realpathSync } from 'node:fs';
import { resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

import { YAML } from 'bun';

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

/** @param {unknown} value @returns {string[]} */
function stringsIn(value) {
	if (typeof value === 'string') return [value];
	if (Array.isArray(value)) return value.flatMap(stringsIn);
	if (value && typeof value === 'object') return Object.values(value).flatMap(stringsIn);
	return [];
}

/** @param {string} profile @param {string} cwd */
export function jobNames(profile, cwd = process.cwd()) {
	if (!/^[a-z][a-z0-9-]*$/.test(profile)) throw new Error('Invalid workflow name');
	const workflow = YAML.parse(readFileSync(resolve(cwd, `.github/workflows/${profile}.yml`), 'utf8'));
	const names = [
		...new Set(
			stringsIn(workflow).flatMap(text =>
				[...text.matchAll(/outputs\.jobs\)\[['"]([a-z][a-z0-9-]*)['"]\]/g)].map(match => match[1])
			),
		),
	];
	if (!names.length) throw new Error(`Workflow ${profile} has no planned jobs`);
	return names;
}

/** @param {string} base @param {string} head @param {string} cwd */
export function affectedProjects(base, head, cwd = process.cwd()) {
	/** @type {unknown} */
	const result = JSON.parse(execFileSync('mise', [
		'run',
		'--affected',
		'--affected-committed',
		'--affected-base',
		base,
		'--affected-head',
		head,
		'--affected-json',
		'test',
	], { cwd, encoding: 'utf8', timeout: 60000, maxBuffer: 16 * 1024 * 1024, stdio: ['pipe', 'pipe', 'pipe'] }));
	if (!result || typeof result !== 'object' || !('projects' in result) || !Array.isArray(result.projects)) {
		throw new Error('mise returned no affected projects');
	}
	/** @type {string[]} */
	const projects = [];
	for (const project of result.projects) {
		if (!project || typeof project !== 'object' || !('id' in project) || typeof project.id !== 'string') {
			throw new Error('mise returned a project without an ID');
		}
		projects.push(project.id);
	}
	return projects;
}

/** @param {string} project @param {string} profile @returns {string[] | null} */
function projectChecks(project, profile) {
	if (project === 'custom:runner-host' || project.startsWith('cargo:')) {
		return profile === 'ci' ? ['plugins'] : profile === 'conformance' ? ['conformance'] : [];
	}
	if (project === 'custom:documentation') return profile === 'ci' ? ['release', 'lint'] : [];
	if (project === 'custom:distribution') {
		return profile === 'ci'
			? ['nix', 'release', 'docker', 'action', 'javascript']
			: profile === 'codeql'
			? ['javascript-typescript', 'actions']
			: [];
	}
	if (project.startsWith('node:')) {
		if (profile === 'codeql') return ['javascript-typescript'];
		if (profile !== 'ci') return [];
		const jobs = ['javascript', 'release'];
		if (project === 'node:actionlint-action') jobs.push('action', 'action-tests', 'docker');
		if (project === 'node:actionlint-playground' || project === 'node:@kjanat-actionlint/language-server') {
			jobs.push('wasm');
		}
		if (
			project.startsWith('node:@kjanat-actionlint/plugin-')
			|| project === 'node:@kjanat-actionlint/external-linter-examples'
		) jobs.push('plugins');
		return jobs;
	}
	// Unknown projects select every check until their CI jobs have been assigned.
	return null;
}

/** @param {string} profile @param {string[]} names @param {string[]} projects @param {{full: boolean, skip: string[]}} trailers */
export function selectJobs(profile, names, projects, trailers = { full: false, skip: [] }) {
	const checks = projects.map(project => projectChecks(project, profile));
	return Object.fromEntries(
		names.map(job => [
			job,
			trailers.full
			|| (!trailers.skip.includes(job) && checks.some(selected => selected === null || selected.includes(job))),
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
		return { paths: [], base: '', head: '', trailers: { full: true, skip: [] } };
	}
	const paths = git(['diff', '--name-only', '--no-renames', '-z', base, head, '--']).split('\0').filter(Boolean);
	return { paths, base, head, trailers };
}

/** @param {NodeJS.ProcessEnv} environment @param {string} cwd @param {typeof affectedProjects} affected */
export function plan(environment = process.env, cwd = process.cwd(), affected = affectedProjects) {
	const profile = required(environment, 'CI_PROFILE');
	const names = jobNames(profile, cwd);
	let jobs;
	let fallback = false;
	try {
		const { paths, base, head, trailers } = changedInputs(environment, cwd);
		const projects = trailers.full || paths.length === 0 ? [] : affected(base, head, cwd);
		jobs = selectJobs(profile, names, projects, trailers);
	} catch (error) {
		console.error(`CI dependency planning failed: ${error instanceof Error ? error.message : String(error)}`);
		jobs = Object.fromEntries(names.map(job => [job, true]));
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
