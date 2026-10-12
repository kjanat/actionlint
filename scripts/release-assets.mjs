// Preserve GoReleaser outputs between draft preparation and publication.
import { constants } from 'node:fs';
import { copyFile, mkdir, readFile, realpath } from 'node:fs/promises';
import { basename, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

const manifests = [
	['homebrew/Casks/actionlint.rb', 'actionlint-homebrew.rb'],
	['scoop/bucket/actionlint.json', 'actionlint-scoop.json'],
	['aur/actionlint-kjanat-bin.pkgbuild', 'actionlint-kjanat-bin.pkgbuild'],
	['aur/actionlint-kjanat-bin.srcinfo', 'actionlint-kjanat-bin.srcinfo'],
	['aur/actionlint-kjanat.pkgbuild', 'actionlint-kjanat.pkgbuild'],
	['aur/actionlint-kjanat.srcinfo', 'actionlint-kjanat.srcinfo'],
];
const uploadable = new Set(['Archive', 'File', 'Source', 'Checksum', 'SBOM', 'Signature', 'Certificate']);
const wingetManifests = new Set([
	'kjanat.actionlint.yaml',
	'kjanat.actionlint.installer.yaml',
	'kjanat.actionlint.locale.en-US.yaml',
]);

/** @param {string} root @param {string} path */
async function inside(root, path) {
	const resolved = await realpath(path);
	const part = relative(await realpath(root), resolved);
	if (part === '..' || part.startsWith(`..${sep}`) || isAbsolute(part)) {
		throw new Error(`Artifact is outside ${root}: ${path}`);
	}
	return resolved;
}

/** @param {string} root @param {string} directory */
export async function collectAssets(root, directory) {
	const dist = join(root, 'dist');
	const artifacts = JSON.parse(await readFile(join(dist, 'artifacts.json'), 'utf8'));
	if (!Array.isArray(artifacts)) throw new Error('Expected GoReleaser artifact array');
	await mkdir(directory, { recursive: true });
	let archives = 0;
	let checksums = 0;
	const winget = new Set();
	for (const artifact of artifacts) {
		if (!artifact || typeof artifact !== 'object') throw new Error('Invalid GoReleaser artifact');
		const isWinget = artifact.type === 'Winget Manifest';
		if (!uploadable.has(artifact.type) && artifact.internal_type !== 2 && !isWinget) continue;
		if (
			typeof artifact.name !== 'string' || !artifact.name || basename(artifact.name) !== artifact.name
			|| /[\\/\0\r\n]/.test(artifact.name)
		) {
			throw new Error('Invalid release asset name');
		}
		if (isWinget) {
			if (!wingetManifests.has(artifact.name)) throw new Error(`Unexpected WinGet package file: ${artifact.name}`);
			winget.add(artifact.name);
		}
		if (typeof artifact.path !== 'string') throw new Error('Missing release asset path');
		const source = resolve(root, artifact.path);
		const destination = resolve(directory, artifact.name);
		// The builder already staged extra_files into the destination.
		if (source !== destination) await copyFile(await inside(dist, source), destination, constants.COPYFILE_EXCL);
		if (artifact.type === 'Archive') archives++;
		if (artifact.type === 'Checksum') checksums++;
	}
	if (!archives || !checksums) throw new Error('GoReleaser produced no release archives or checksums');
	if (winget.size !== wingetManifests.size) {
		throw new Error('GoReleaser did not generate all three WinGet package files');
	}
	for (const [source, name] of manifests) {
		await copyFile(await inside(dist, join(dist, source)), join(directory, name), constants.COPYFILE_EXCL);
	}
}

/** @param {typeof fetch} request @param {string} token @param {string} endpoint @param {object | undefined} body @param {string} method */
async function github(request, token, endpoint, body, method = body ? 'PUT' : 'GET') {
	const response = await request(`https://api.github.com/${endpoint}`, {
		method,
		headers: {
			Authorization: `Bearer ${token}`,
			Accept: 'application/vnd.github+json',
			'Content-Type': 'application/json',
			'X-GitHub-Api-Version': '2022-11-28',
		},
		redirect: 'error',
		signal: AbortSignal.timeout(30_000),
		...(body ? { body: JSON.stringify(body) } : {}),
	});
	if (response.status === 404 && method === 'GET') return undefined;
	if (!response.ok) throw new Error(`GitHub ${method} ${endpoint}: HTTP ${response.status}`);
	if (response.status === 204) return undefined;
	return response.json();
}

/** @param {unknown} value */
function objectSHA(value) {
	if (typeof value !== 'string' || !/^[a-f0-9]{40}$/.test(value)) throw new Error('Invalid GitHub object SHA');
	return value;
}

const wingetPrefix = 'manifests/k/kjanat/actionlint/';
const wingetRepository = 'repos/microsoft/winget-pkgs';
const wingetFork = 'repos/kjanat/winget-pkgs';
const wingetLock = 'heads/actionlint-winget-submission-lock';

/** @param {string} token @param {string} query @param {Record<string, unknown>} variables @param {typeof fetch} request */
async function wingetGraphQL(token, query, variables, request) {
	const result = await github(request, token, 'graphql', { query, variables }, 'POST');
	if (!result?.data || result.errors?.length) throw new Error('Could not inspect WinGet pull requests through GraphQL');
	return result.data;
}

/** Inspect changed files directly; PR titles and search indexing are not authoritative.
 * @param {string} token @param {typeof fetch} request
 * @returns {Promise<{number: number, url: string}[]>}
 */
async function openWingetSubmissions(token, request) {
	const query = `query($cursor: String) {
		repository(owner: "microsoft", name: "winget-pkgs") {
			pullRequests(first: 50, after: $cursor, states: OPEN, orderBy: {field: CREATED_AT, direction: ASC}) {
				totalCount pageInfo { hasNextPage endCursor }
				nodes { number url files(first: 100) { totalCount pageInfo { hasNextPage endCursor } nodes { path } } }
			}
		}
	}`;
	const filesQuery = `query($number: Int!, $cursor: String!) {
		repository(owner: "microsoft", name: "winget-pkgs") {
			pullRequest(number: $number) {
				files(first: 100, after: $cursor) { totalCount pageInfo { hasNextPage endCursor } nodes { path } }
			}
		}
	}`;
	/** @type {{number: number, url: string}[]} */
	const matches = [];
	/** @type {string | null} */
	let cursor = null;
	const cursors = new Set();
	const seenPulls = new Set();
	let inspected = 0;
	let expected;
	do {
		const data = await wingetGraphQL(token, query, { cursor }, request);
		const pulls = data.repository?.pullRequests;
		checkConnection(pulls);
		expected ??= pulls.totalCount;
		if (pulls.totalCount !== expected) throw new Error('Open WinGet PRs changed during inspection; submission stopped');
		for (const pull of pulls.nodes) {
			if (
				!Number.isSafeInteger(pull.number) || pull.number < 1
				|| pull.url !== `https://github.com/microsoft/winget-pkgs/pull/${pull.number}`
			) throw new Error('Invalid WinGet pull request response');
			if (seenPulls.has(pull.number)) throw new Error('Repeated WinGet PR during inspection');
			seenPulls.add(pull.number);
			let files = pull.files;
			const fileCursors = new Set();
			const seenFiles = new Set();
			let fileCount = 0;
			let fileTotal;
			let matchesPackage = false;
			for (;;) {
				checkConnection(files);
				fileTotal ??= files.totalCount;
				if (files.totalCount !== fileTotal) throw new Error('WinGet PR files changed during inspection');
				for (const file of files.nodes) {
					if (typeof file.path !== 'string') throw new Error('Invalid WinGet changed file');
					if (seenFiles.has(file.path)) throw new Error('Repeated WinGet PR file during inspection');
					seenFiles.add(file.path);
					if (file.path.toLowerCase().startsWith(wingetPrefix)) matchesPackage = true;
				}
				fileCount += files.nodes.length;
				if (!files.pageInfo.hasNextPage) break;
				if (fileCursors.size >= 10_000) throw new Error('Incomplete WinGet PR file inspection');
				const next = nextCursor(files, fileCursors);
				const more = await wingetGraphQL(token, filesQuery, { number: pull.number, cursor: next }, request);
				files = more.repository?.pullRequest?.files;
			}
			if (fileCount !== fileTotal) throw new Error('Incomplete WinGet PR file inspection');
			if (matchesPackage) matches.push({ number: pull.number, url: pull.url });
		}
		inspected += pulls.nodes.length;
		if (!pulls.pageInfo.hasNextPage) break;
		cursor = nextCursor(pulls, cursors);
	} while (cursors.size < 10_000);
	if (inspected !== expected) throw new Error('Incomplete open WinGet PR inspection');
	return matches;
}

/** @param {any} connection */
function checkConnection(connection) {
	if (
		!connection || !Array.isArray(connection.nodes) || !Number.isSafeInteger(connection.totalCount)
		|| connection.totalCount < 0 || typeof connection.pageInfo?.hasNextPage !== 'boolean'
		|| (connection.pageInfo.hasNextPage && !connection.nodes.length)
	) throw new Error('Invalid WinGet pagination response');
}

/** @param {any} connection @param {Set<string>} seen */
function nextCursor(connection, seen) {
	const cursor = connection.pageInfo.endCursor;
	if (typeof cursor !== 'string' || !cursor || seen.has(cursor)) throw new Error('Invalid WinGet pagination cursor');
	seen.add(cursor);
	return cursor;
}

/** @param {string} token @param {typeof fetch} request */
async function requireNoWingetSubmission(token, request) {
	const existing = await openWingetSubmissions(token, request);
	if (existing.length) {
		throw new Error(`WinGet submission blocked by existing package PRs: ${existing.map(p => p.url).join(', ')}`);
	}
}

/** @param {string} token @param {string} version @param {{path: string, content: string}[]} files @param {typeof fetch} request */
async function wingetAlreadyPublished(token, version, files, request) {
	let published = 0;
	for (const file of files) {
		const previous = await github(request, token, `${wingetRepository}/contents/${file.path}?ref=master`, undefined);
		if (!previous) continue;
		if (
			previous.encoding !== 'base64' || typeof previous.content !== 'string'
			|| !Buffer.from(previous.content, 'base64').equals(Buffer.from(file.content))
		) {
			throw new Error(`WinGet already has different contents for ${file.path}. Check that file before retrying.`);
		}
		published++;
	}
	if (published && published !== files.length) {
		throw new Error(`WinGet is missing some package files for ${version}. Check the published files before retrying.`);
	}
	return published === files.length;
}

/** Submit the release's WinGet package files once, after complete duplicate checks.
 * @param {string} directory @param {string} version @param {NodeJS.ProcessEnv} env @param {typeof fetch} request
 */
export async function publishWinget(directory, version, env = process.env, request = fetch) {
	if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Expected stable MAJOR.MINOR.PATCH version');
	const token = env.WINGET_TOKEN;
	if (!token) throw new Error('Missing WINGET_TOKEN. Set this repository secret before submitting to WinGet.');
	const branch = `actionlint-${version}`;
	const attempt = `tags/actionlint-winget-attempt-${version}`;
	const path = `${wingetPrefix}${version}`;
	const files = await Promise.all([...wingetManifests.values()].map(async (name) => ({
		path: `${path}/${name}`,
		mode: '100644',
		type: 'blob',
		content: new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(
			await readFile(await inside(directory, join(directory, name))),
		),
	})));
	// Skip versions that Microsoft already has with the same package files.
	if (await wingetAlreadyPublished(token, version, files, request)) {
		console.info(`WinGet already has kjanat.actionlint ${version}; no update needed.`);
		return;
	}
	if (await github(request, token, `${wingetFork}/git/ref/${attempt}`, undefined)) {
		throw new Error(`WinGet submission for ${version} was already attempted; no second attempt will be made`);
	}
	if (await github(request, token, `${wingetFork}/git/ref/heads/${branch}`, undefined)) {
		throw new Error(`WinGet submission branch ${branch} already exists; check the previous submission`);
	}
	const prior = await github(
		request,
		token,
		`${wingetRepository}/pulls?state=all&head=kjanat:${branch}&base=master&per_page=100`,
		undefined,
	);
	if (!Array.isArray(prior)) throw new Error('Unexpected WinGet pull request history');
	if (prior.length) {
		throw new Error(`WinGet submission for ${version} already exists: ${prior.map(p => p.html_url).join(', ')}`);
	}
	const parent = await github(request, token, `${wingetRepository}/git/ref/heads/master`, undefined);
	const parentSHA = objectSHA(parent?.object?.sha);
	// Creating this shared reference is atomic and serializes all cooperating submitters, across versions.
	try {
		await github(request, token, `${wingetFork}/git/refs`, { ref: `refs/${wingetLock}`, sha: parentSHA }, 'POST');
	} catch (error) {
		throw new Error(
			`Could not acquire the shared WinGet submission lock. Check for a running or interrupted publisher: ${
				String(error)
			}`,
		);
	}
	let releaseLock = true;
	/** @type {unknown} */
	let submissionError;
	try {
		await requireNoWingetSubmission(token, request);
		const commit = await github(request, token, `${wingetFork}/git/commits/${parentSHA}`, undefined);
		const baseTree = objectSHA(commit?.tree?.sha);
		const tree = await github(request, token, `${wingetFork}/git/trees`, { base_tree: baseTree, tree: files }, 'POST');
		const treeSHA = objectSHA(tree?.sha);
		const next = await github(request, token, `${wingetFork}/git/commits`, {
			message: `New version: kjanat.actionlint ${version}`,
			tree: treeSHA,
			parents: [parentSHA],
		}, 'POST');
		const sha = objectSHA(next?.sha);
		await github(request, token, `${wingetFork}/git/refs`, { ref: `refs/heads/${branch}`, sha }, 'POST');
		await requireNoWingetSubmission(token, request);
		if (await wingetAlreadyPublished(token, version, files, request)) {
			throw new Error(`WinGet version ${version} was published during preparation; no submission was made`);
		}
		// Keep the attempt tag permanently, including after timeouts, closed PRs and branch deletion.
		await github(request, token, `${wingetFork}/git/refs`, { ref: `refs/${attempt}`, sha }, 'POST');
		// An uncertain creation or withdrawal retains the global lock for maintainer inspection.
		releaseLock = false;
		const pull = await github(request, token, `${wingetRepository}/pulls`, {
			title: `New version: kjanat.actionlint ${version}`,
			head: `kjanat:${branch}`,
			base: 'master',
			draft: false,
			body: `Manifests generated by GoReleaser for https://github.com/kjanat/actionlint/releases/tag/v${version}.`,
		}, 'POST');
		if (
			!Number.isSafeInteger(pull?.number) || pull.number < 1
			|| pull.html_url !== `https://github.com/microsoft/winget-pkgs/pull/${pull.number}`
		) throw new Error('Unexpected WinGet pull request response');
		try {
			const submissions = await openWingetSubmissions(token, request);
			if (!submissions.some(p => p.number === pull.number)) {
				throw new Error('Could not confirm the new WinGet submission');
			}
			const others = submissions.filter(p => p.number !== pull.number);
			if (others.length) throw new Error(`Concurrent WinGet submission detected: ${others.map(p => p.url).join(', ')}`);
		} catch (error) {
			// Independent contributors cannot share our lock. Withdraw ours if the final check detects a race or fails.
			try {
				const closed = await github(
					request,
					token,
					`${wingetRepository}/pulls/${pull.number}`,
					{ state: 'closed' },
					'PATCH',
				);
				if (closed?.state !== 'closed') throw new Error('GitHub did not confirm PR closure');
			} catch (closeError) {
				throw new Error(`${String(error)}; could not withdraw ${pull.html_url}: ${String(closeError)}`);
			}
			releaseLock = true;
			throw error;
		}
		releaseLock = true;
		console.info(`WinGet PR: ${pull.html_url}`);
	} catch (error) {
		submissionError = releaseLock
			? (error instanceof Error ? error : new Error(String(error)))
			: new Error(
				`${
					String(error)
				}; the shared submission lock has been retained because the PR state is uncertain. Inspect WinGet before removing it.`,
			);
	} finally {
		if (releaseLock) {
			try {
				await github(request, token, `${wingetFork}/git/refs/${wingetLock}`, undefined, 'DELETE');
			} catch (cleanupError) {
				submissionError = new Error(
					`${
						submissionError ? `${String(submissionError)}; ` : ''
					}could not release the shared WinGet submission lock: ${String(cleanupError)}`,
				);
			}
		}
	}
	if (submissionError) throw submissionError;
}

/** Open one actionable failure issue per version; reruns reuse or reopen it.
 * @param {string} version @param {unknown} error @param {NodeJS.ProcessEnv} env @param {typeof fetch} request
 */
export async function reportWingetFailure(version, error, env = process.env, request = fetch) {
	const token = env.GITHUB_TOKEN;
	if (!token) throw new Error('Missing GITHUB_TOKEN for reporting the WinGet failure');
	const release = /^\d+\.\d+\.\d+$/.test(version) ? version : 'invalid-version';
	const marker = `<!-- actionlint-winget-failure:${release} -->`;
	const repository = 'repos/kjanat/actionlint';
	let message = error instanceof Error ? error.message : String(error);
	for (const secret of [env.WINGET_TOKEN, env.GITHUB_TOKEN]) {
		if (secret) message = message.replaceAll(secret, '[redacted]');
	}
	const run = /^\d+$/.test(env.GITHUB_RUN_ID ?? '')
		? `https://github.com/kjanat/actionlint/actions/runs/${env.GITHUB_RUN_ID}`
		: 'Check the Release workflow log.';
	const body =
		`${marker}\nWinGet submission for kjanat.actionlint ${release} failed or was blocked.\n\n${message}\n\nWorkflow: ${run}\n\nCheck existing WinGet PRs and the submission log before taking further action. The automated publisher will not repeat a recorded submission attempt for this version.`;
	for (let page = 1; page <= 10_000; page++) {
		const issues = await github(request, token, `${repository}/issues?state=all&per_page=100&page=${page}`, undefined);
		if (!Array.isArray(issues)) throw new Error('Could not inspect existing WinGet failure issues');
		const existing = issues.find(issue =>
			!issue.pull_request && typeof issue.body === 'string' && issue.body.includes(marker)
		);
		if (existing) {
			if (!Number.isSafeInteger(existing.number) || existing.number < 1) {
				throw new Error('Invalid failure issue response');
			}
			if (existing.state === 'closed') {
				await github(request, token, `${repository}/issues/${existing.number}`, { state: 'open', body }, 'PATCH');
			}
			console.info(`WinGet failure issue: https://github.com/kjanat/actionlint/issues/${existing.number}`);
			return;
		}
		if (issues.length < 100) {
			const issue = await github(request, token, `${repository}/issues`, {
				title: `WinGet submission failed: kjanat.actionlint ${release}`,
				body,
			}, 'POST');
			if (!Number.isSafeInteger(issue?.number) || issue.number < 1) {
				throw new Error('Invalid new failure issue response');
			}
			console.info(`WinGet failure issue: https://github.com/kjanat/actionlint/issues/${issue.number}`);
			return;
		}
	}
	throw new Error('Incomplete WinGet failure issue inspection');
}

/** @param {string} directory @param {string} version @param {NodeJS.ProcessEnv} env @param {typeof fetch} request */
export async function publishWingetAndReport(directory, version, env = process.env, request = fetch) {
	try {
		await publishWinget(directory, version, env, request);
	} catch (error) {
		try {
			await reportWingetFailure(version, error, env, request);
		} catch (reportError) {
			throw new Error(
				`WinGet submission failed: ${String(error)}; failure issue could not be created: ${String(reportError)}`,
			);
		}
		throw error;
	}
}

/** @param {string} directory @param {string} version @param {NodeJS.ProcessEnv} env @param {typeof fetch} request */
export async function publishManifests(directory, version, env = process.env, request = fetch) {
	if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Expected stable MAJOR.MINOR.PATCH version');
	const targets = [
		{
			repo: 'homebrew-tap',
			path: 'Casks/actionlint.rb',
			asset: 'actionlint-homebrew.rb',
			token: env.HOMEBREW_TAP_TOKEN,
		},
		{
			repo: 'scoop-bucket',
			path: 'bucket/actionlint.json',
			asset: 'actionlint-scoop.json',
			token: env.SCOOP_BUCKET_TOKEN,
		},
	];
	for (const target of targets) if (!target.token) throw new Error(`Missing publication token for ${target.repo}`);
	for (const target of targets) {
		const token = target.token;
		if (!token) throw new Error(`Missing publication token for ${target.repo}`);
		const bytes = await readFile(join(directory, target.asset));
		const endpoint = `repos/kjanat/${target.repo}/contents/${target.path}`;
		const previous = await github(request, token, `${endpoint}?ref=master`, undefined);
		if (
			previous
			&& (typeof previous.sha !== 'string' || typeof previous.content !== 'string' || previous.encoding !== 'base64')
		) {
			throw new Error(`Unexpected contents response for ${target.repo}`);
		}
		if (previous && Buffer.from(previous.content, 'base64').equals(bytes)) continue;
		await github(request, token, endpoint, {
			message: `Update actionlint to v${version}`,
			branch: 'master',
			content: bytes.toString('base64'),
			...(previous ? { sha: previous.sha } : {}),
		});
	}
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	try {
		const [command, directory, version] = process.argv.slice(2);
		if (command === 'report-winget-failure' && directory) {
			await reportWingetFailure(
				directory,
				'The WinGet submission job failed. See the linked workflow log for details.',
			);
		} else {
			if (!directory || !['collect', 'publish', 'publish-winget'].includes(command)) {
				throw new Error(
					'Usage: release-assets.mjs collect DIRECTORY | {publish|publish-winget} DIRECTORY VERSION | report-winget-failure VERSION',
				);
			}
			if (command === 'collect') await collectAssets(process.cwd(), resolve(directory));
			else if (command === 'publish-winget') await publishWingetAndReport(resolve(directory), version);
			else await publishManifests(resolve(directory), version);
		}
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
