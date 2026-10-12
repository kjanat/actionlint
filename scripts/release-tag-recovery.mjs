import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function object(value) {
	return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/** @param {unknown} value @returns {value is number} */
function identifier(value) {
	return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

/** @param {string[]} args */
function gh(args) {
	return execFileSync('gh', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] });
}

/** @param {string} text @param {string} key @returns {Record<string, unknown>[]} */
function records(text, key) {
	/** @type {unknown} */
	const pages = JSON.parse(text);
	if (!Array.isArray(pages)) throw new Error('Expected paginated GitHub response');
	return pages.flatMap((page) => {
		if (!object(page) || !Array.isArray(page[key]) || !page[key].every(object)) {
			throw new Error(`Invalid GitHub ${key} response`);
		}
		return page[key];
	});
}

/**
 * Require the release's own prerequisite gate to have admitted the floating-tag push.
 * @param {string} repository @param {string} tag @param {string} commit
 * @param {(args: string[]) => string} request
 */
export function verifyTagRecovery(repository, tag, commit, request = gh) {
	if (!/^[\w.-]+\/[\w.-]+$/.test(repository) || repository.trim() !== repository) throw new Error('Invalid repository');
	if (!/^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(tag) || tag.trim() !== tag) {
		throw new Error('Invalid stable release tag');
	}
	if (!/^[a-f0-9]{40}$/.test(commit) || commit.trim() !== commit) throw new Error('Invalid release commit');
	const root = `repos/${repository}/actions`;
	const runs = records(
		request([
			'api',
			'--paginate',
			'--slurp',
			`${root}/workflows/release.yml/runs?event=release&status=completed&head_sha=${commit}&per_page=100`,
		]),
		'workflow_runs',
	);
	for (const run of runs) {
		if (
			run.head_sha !== commit || run.head_branch !== tag || run.path !== '.github/workflows/release.yml'
			|| run.event !== 'release' || run.status !== 'completed'
			|| !['success', 'failure'].includes(String(run.conclusion))
			|| !identifier(run.id) || !identifier(run.run_attempt)
		) continue;
		const jobs = records(
			request([
				'api',
				'--paginate',
				'--slurp',
				`${root}/runs/${run.id}/attempts/${run.run_attempt}/jobs?per_page=100`,
			]),
			'jobs',
		);
		// release.yml admits this step only after verification, npm, Docker, every
		// Action smoke job, and successful (or ineligible) distribution publication.
		const admitted = jobs.filter((job) =>
			job.name === 'Update moving Action references' && job.run_id === run.id && job.head_sha === commit
			&& job.status === 'completed' && ['success', 'failure'].includes(String(job.conclusion))
			&& Array.isArray(job.steps) && job.steps.some((step) =>
				object(step) && step.name === 'Update eligible floating tags' && step.status === 'completed'
				&& ['success', 'failure'].includes(String(step.conclusion))
			)
		);
		if (admitted.length === 1) return;
	}
	throw new Error(
		'The selected tag and commit need a completed Release attempt that reached the floating-tag push after all publication checks passed',
	);
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	const [repository, tag, commit, ...extra] = process.argv.slice(2);
	if (!repository || !tag || !commit || extra.length) {
		throw new Error('Usage: release-tag-recovery.mjs REPOSITORY TAG COMMIT');
	}
	verifyTagRecovery(repository, tag, commit);
}
