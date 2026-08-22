// @ts-check

import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

/** @typedef {import('@actions/github-script').AsyncFunctionArguments} AsyncFunctionArguments */

/**
 * @typedef VersionStatus
 * @property {string} shellcheckVersion
 * @property {string} goShellcheckVersion
 * @property {string} embeddedShellcheckVersion
 * @property {string} pinnedGoShellcheckVersion
 * @property {boolean} pinnedOutdated
 * @property {boolean} wrapperLagging
 * @property {boolean} filesUpdated
 * @property {ReturnType<typeof versionVerdict>} verdict
 */

export const TRACKING_MARKER = '<!-- go-shellcheck-shellcheck-version-tracker -->';

const DEPENDENCY = 'github.com/wasilibs/go-shellcheck/cmd/shellcheck@';
const VERSION_PATTERN = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
export const VERSION_FILES = [
	'.pre-commit-hooks.yaml',
	'docs/usage.md',
	'scripts/bump-version/targets.go',
];

/**
 * Parse a stable version tag.
 *
 * @param {string} value
 * @returns {[number, number, number]}
 */
export function parseVersion(value) {
	const match = VERSION_PATTERN.exec(value);
	if (match === null) {
		throw new Error(`expected a stable vMAJOR.MINOR.PATCH version, got ${JSON.stringify(value)}`);
	}
	return [Number(match[1]), Number(match[2]), Number(match[3])];
}

/**
 * @param {string} left
 * @param {string} right
 * @returns {-1 | 0 | 1}
 */
export function compareVersions(left, right) {
	const leftParts = parseVersion(left);
	const rightParts = parseVersion(right);
	for (let i = 0; i < leftParts.length; i++) {
		if (leftParts[i] !== rightParts[i]) {
			return leftParts[i] < rightParts[i] ? -1 : 1;
		}
	}
	return 0;
}

/**
 * @param {boolean} pinnedOutdated
 * @param {boolean} wrapperLagging
 * @returns {'current' | 'dependency-update-available' | 'wrapper-update-needed' | 'dependency-update-available-and-wrapper-update-needed'}
 */
export function versionVerdict(pinnedOutdated, wrapperLagging) {
	if (pinnedOutdated && wrapperLagging) {
		return 'dependency-update-available-and-wrapper-update-needed';
	}
	if (pinnedOutdated) {
		return 'dependency-update-available';
	}
	if (wrapperLagging) {
		return 'wrapper-update-needed';
	}
	return 'current';
}

/**
 * @param {Map<string, string>} files
 * @param {string} latestVersion
 */
export function planVersionUpdate(files, latestVersion) {
	parseVersion(latestVersion);
	/** @type {Map<string, string>} */
	const found = new Map();
	for (const file of VERSION_FILES) {
		const contents = files.get(file);
		if (contents === undefined) {
			throw new Error(`missing version source ${file}`);
		}
		const matches = [
			...contents.matchAll(
				/github\.com\/wasilibs\/go-shellcheck\/cmd\/shellcheck@(v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))/g,
			),
		];
		if (matches.length !== 1) {
			throw new Error(`expected one pinned go-shellcheck dependency in ${file}, found ${matches.length}`);
		}
		found.set(file, matches[0][1]);
	}

	const currentVersions = new Set(found.values());
	if (currentVersions.size !== 1) {
		throw new Error(
			`go-shellcheck pins disagree: ${[...found].map(([file, version]) => `${file}=${version}`).join(', ')}`,
		);
	}
	const currentVersion = currentVersions.values().next().value;
	if (currentVersion === undefined) {
		throw new Error('go-shellcheck pin is missing');
	}

	const comparison = compareVersions(latestVersion, currentVersion);
	if (comparison < 0) {
		throw new Error(`latest go-shellcheck release ${latestVersion} is older than pinned ${currentVersion}`);
	}
	if (comparison === 0) {
		return { changed: false, currentVersion, files };
	}

	/** @type {Map<string, string>} */
	const updated = new Map();
	for (const [file, contents] of files) {
		const currentDependency = `${DEPENDENCY}${currentVersion}`;
		const nextDependency = `${DEPENDENCY}${latestVersion}`;
		updated.set(file, contents.replace(currentDependency, nextDependency));
	}
	return { changed: true, currentVersion, files: updated };
}

/**
 * @param {unknown} release
 * @param {string} repository
 */
function releaseVersion(release, repository) {
	const response = /** @type {{ data?: { tag_name?: unknown } }} */ (release);
	const tag = response.data?.tag_name;
	if (typeof tag !== 'string') {
		throw new Error(`${repository} latest release did not contain a tag name`);
	}
	parseVersion(tag);
	return tag;
}

/**
 * @param {unknown} response
 * @param {string} repository
 * @param {string} file
 */
function decodedContent(response, repository, file) {
	const decoded = /** @type {{ data?: unknown }} */ (response).data;
	if (
		decoded === null
		|| typeof decoded !== 'object'
		|| Array.isArray(decoded)
		|| !('content' in decoded)
		|| typeof decoded.content !== 'string'
	) {
		throw new Error(`${repository}/${file} was not a file`);
	}
	if (!('encoding' in decoded) || decoded.encoding !== 'base64') {
		const encoding = 'encoding' in decoded ? decoded.encoding : undefined;
		throw new Error(`${repository}/${file} used unsupported encoding ${JSON.stringify(encoding)}`);
	}
	return Buffer.from(decoded.content, 'base64').toString('utf8').trim();
}

/**
 * Check upstream and pinned versions, optionally applying the local pin update.
 *
 * @param {{
 *   github: AsyncFunctionArguments['github'],
 *   core: AsyncFunctionArguments['core'],
 *   workspace?: string,
 *   updateFiles?: boolean,
 *   failOnOutdated?: boolean,
 * }} options
 * @returns {Promise<VersionStatus>}
 */
export async function checkVersions({
	github,
	core,
	workspace = process.cwd(),
	updateFiles = false,
	failOnOutdated = false,
}) {
	const [shellcheckRelease, goShellcheckRelease] = await Promise.all([
		github.rest.repos.getLatestRelease({ owner: 'koalaman', repo: 'shellcheck' }),
		github.rest.repos.getLatestRelease({ owner: 'wasilibs', repo: 'go-shellcheck' }),
	]);
	const shellcheckVersion = releaseVersion(shellcheckRelease, 'koalaman/shellcheck');
	const goShellcheckVersion = releaseVersion(goShellcheckRelease, 'wasilibs/go-shellcheck');
	const embeddedResponse = await github.rest.repos.getContent({
		owner: 'wasilibs',
		repo: 'go-shellcheck',
		path: 'buildtools/wasm/version.txt',
		ref: goShellcheckVersion,
	});
	const embeddedShellcheckVersion = decodedContent(
		embeddedResponse,
		'wasilibs/go-shellcheck',
		'buildtools/wasm/version.txt',
	);
	parseVersion(embeddedShellcheckVersion);

	if (compareVersions(embeddedShellcheckVersion, shellcheckVersion) > 0) {
		throw new Error(
			`go-shellcheck ${goShellcheckVersion} embeds ${embeddedShellcheckVersion}, newer than the latest ShellCheck release ${shellcheckVersion}`,
		);
	}

	const files = new Map(
		await Promise.all(
			VERSION_FILES.map(async (
				file,
			) => /** @type {[string, string]} */ ([file, await readFile(path.join(workspace, file), 'utf8')])),
		),
	);
	const update = planVersionUpdate(files, goShellcheckVersion);
	const pinnedOutdated = update.changed;
	const wrapperLagging = compareVersions(shellcheckVersion, embeddedShellcheckVersion) > 0;
	const filesUpdated = updateFiles && pinnedOutdated;
	const verdict = versionVerdict(pinnedOutdated, wrapperLagging);

	if (filesUpdated) {
		for (const [file, contents] of update.files) {
			await writeFile(path.join(workspace, file), contents, 'utf8');
		}
	}

	core.setOutput('verdict', verdict);
	core.setOutput('shellcheck-version', shellcheckVersion);
	core.setOutput('go-shellcheck-version', goShellcheckVersion);
	core.setOutput('embedded-shellcheck-version', embeddedShellcheckVersion);
	core.setOutput('pinned-go-shellcheck-version', update.currentVersion);
	core.setOutput('pinned-outdated', String(pinnedOutdated));
	core.setOutput('wrapper-lagging', String(wrapperLagging));
	core.setOutput('files-updated', String(filesUpdated));
	/** @type {Array<Array<string | { data: string, header?: boolean }>>} */
	const versionTable = [
		[
			{ data: 'Component', header: true },
			{ data: 'Version', header: true },
		],
		['ShellCheck release', shellcheckVersion],
		['go-shellcheck release', goShellcheckVersion],
		['ShellCheck embedded by go-shellcheck', embeddedShellcheckVersion],
		['Pinned go-shellcheck dependency', update.currentVersion],
	];
	await core.summary
		.addHeading('ShellCheck version status')
		.addTable(versionTable)
		.addRaw(`\nVerdict: ${verdict}\n`)
		.addRaw(`\nLocal pin outdated: ${pinnedOutdated ? 'yes' : 'no'}\n`)
		.addRaw(`\ngo-shellcheck behind ShellCheck: ${wrapperLagging ? 'yes' : 'no'}\n`)
		.write();

	core.info(
		`ShellCheck ${shellcheckVersion}; go-shellcheck ${goShellcheckVersion} embeds ${embeddedShellcheckVersion}; pinned ${update.currentVersion}; verdict ${verdict}`,
	);

	/** @type {VersionStatus} */
	const status = {
		shellcheckVersion,
		goShellcheckVersion,
		embeddedShellcheckVersion,
		pinnedGoShellcheckVersion: update.currentVersion,
		pinnedOutdated,
		wrapperLagging,
		filesUpdated,
		verdict,
	};
	if (failOnOutdated && pinnedOutdated) {
		throw new Error(
			`pinned go-shellcheck ${update.currentVersion} is outdated; latest release is ${goShellcheckVersion}`,
		);
	}
	return status;
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @param {string} name
 */
function requiredVersion(env, name) {
	const value = env[name];
	if (typeof value !== 'string') {
		throw new Error(`${name} is not set`);
	}
	parseVersion(value);
	return value;
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @param {string} name
 */
function requiredBoolean(env, name) {
	const value = env[name];
	if (value !== 'true' && value !== 'false') {
		throw new Error(`${name} must be true or false`);
	}
	return value === 'true';
}

/**
 * Reusable-action entry point.
 *
 * @param {AsyncFunctionArguments} args
 */
export default async function run({ github, core }) {
	return checkVersions({
		github,
		core,
		updateFiles: requiredBoolean(process.env, 'UPDATE_FILES'),
		failOnOutdated: requiredBoolean(process.env, 'FAIL_ON_OUTDATED'),
	});
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @param {string} owner
 * @param {string} repo
 */
function optionalPullRequestURL(env, owner, repo) {
	const value = env.BUMP_PR_URL ?? '';
	if (value === '') {
		return '';
	}
	const pattern = new RegExp(`^https://github\\.com/${owner}/${repo}/pull/[1-9]\\d*$`);
	if (!pattern.test(value)) {
		throw new Error(`BUMP_PR_URL is not a pull request URL for ${owner}/${repo}`);
	}
	return value;
}

/** @param {string} shellcheckVersion */
function trackingTitle(shellcheckVersion) {
	return `Track ${shellcheckVersion} support in go-shellcheck`;
}

/** @param {{ shellcheckVersion: string, goShellcheckVersion: string, embeddedShellcheckVersion: string, bumpPullRequestURL: string }} versions */
function trackingBody({ shellcheckVersion, goShellcheckVersion, embeddedShellcheckVersion, bumpPullRequestURL }) {
	const pullRequest = bumpPullRequestURL === '' ? '' : `\n\nLocal dependency update: ${bumpPullRequestURL}`;
	return `${TRACKING_MARKER}

[ShellCheck ${shellcheckVersion}](https://github.com/koalaman/shellcheck/releases/tag/${shellcheckVersion}) is available, but the latest [go-shellcheck ${goShellcheckVersion}](https://github.com/wasilibs/go-shellcheck/releases/tag/${goShellcheckVersion}) release embeds ShellCheck ${embeddedShellcheckVersion}.

Keep this issue open until a go-shellcheck release embeds ShellCheck ${shellcheckVersion} or newer.${pullRequest}

This issue is maintained by the ShellCheck version lifecycle workflow.`;
}

/**
 * Open, update, reopen, or close the single marker-owned tracking issue.
 *
 * @param {{
 *   github: AsyncFunctionArguments['github'],
 *   context: AsyncFunctionArguments['context'],
 *   core: AsyncFunctionArguments['core'],
 *   env?: NodeJS.ProcessEnv,
 * }} options
 */
export async function reconcileTrackingIssue({ github, context, core, env = process.env }) {
	const { owner, repo } = context.repo;
	const shellcheckVersion = requiredVersion(env, 'SHELLCHECK_VERSION');
	const goShellcheckVersion = requiredVersion(env, 'GO_SHELLCHECK_VERSION');
	const embeddedShellcheckVersion = requiredVersion(env, 'EMBEDDED_SHELLCHECK_VERSION');
	const wrapperLagging = requiredBoolean(env, 'GO_SHELLCHECK_LAGGING');
	const bumpPullRequestURL = optionalPullRequestURL(env, owner, repo);

	const issues = await github.paginate(github.rest.issues.listForRepo, {
		owner,
		repo,
		state: 'all',
		per_page: 100,
	});
	const trackingIssues = issues.filter(
		(issue) =>
			issue.pull_request === undefined
			&& typeof issue.body === 'string'
			&& issue.body.includes(TRACKING_MARKER),
	);
	if (trackingIssues.length > 1) {
		throw new Error(`found ${trackingIssues.length} issues owned by ${TRACKING_MARKER}`);
	}
	const trackingIssue = trackingIssues[0];

	if (wrapperLagging) {
		const title = trackingTitle(shellcheckVersion);
		const body = trackingBody({
			shellcheckVersion,
			goShellcheckVersion,
			embeddedShellcheckVersion,
			bumpPullRequestURL,
		});
		if (trackingIssue === undefined) {
			const created = await github.rest.issues.create({
				owner,
				repo,
				title,
				body,
				labels: ['dependencies', 'area:ci', 'type:task'],
			});
			core.info(`Opened tracking issue ${created.data.html_url}`);
			return;
		}

		if (trackingIssue.state !== 'open' || trackingIssue.title !== title || trackingIssue.body !== body) {
			await github.rest.issues.update({
				owner,
				repo,
				issue_number: trackingIssue.number,
				title,
				body,
				state: 'open',
			});
			core.info(`Updated tracking issue ${trackingIssue.html_url}`);
		} else {
			core.info(`Tracking issue ${trackingIssue.html_url} is current`);
		}
		return;
	}

	if (trackingIssue === undefined || trackingIssue.state !== 'open') {
		core.info('No open go-shellcheck tracking issue needs closing');
		return;
	}
	const pullRequest = bumpPullRequestURL === '' ? '' : ` Dependency update: ${bumpPullRequestURL}.`;
	await github.rest.issues.createComment({
		owner,
		repo,
		issue_number: trackingIssue.number,
		body: `go-shellcheck ${goShellcheckVersion} now embeds ShellCheck ${embeddedShellcheckVersion}.${pullRequest}`,
	});
	await github.rest.issues.update({
		owner,
		repo,
		issue_number: trackingIssue.number,
		state: 'closed',
		state_reason: 'completed',
	});
	core.info(`Closed tracking issue ${trackingIssue.html_url}`);
}
