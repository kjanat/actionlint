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
	for (const artifact of artifacts) {
		if (!artifact || typeof artifact !== 'object') throw new Error('Invalid GoReleaser artifact');
		if (!uploadable.has(artifact.type) && artifact.internal_type !== 2) continue;
		if (
			typeof artifact.name !== 'string' || !artifact.name || basename(artifact.name) !== artifact.name
			|| /[\\/\0\r\n]/.test(artifact.name)
		) {
			throw new Error('Invalid release asset name');
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
	for (const [source, name] of manifests) {
		await copyFile(await inside(dist, join(dist, source)), join(directory, name), constants.COPYFILE_EXCL);
	}
}

/** @param {typeof fetch} request @param {string} token @param {string} endpoint @param {object | undefined} body */
async function github(request, token, endpoint, body) {
	const response = await request(`https://api.github.com/${endpoint}`, {
		method: body ? 'PUT' : 'GET',
		headers: {
			Authorization: `Bearer ${token}`,
			Accept: 'application/vnd.github+json',
			'Content-Type': 'application/json',
			'X-GitHub-Api-Version': '2022-11-28',
		},
		signal: AbortSignal.timeout(30_000),
		...(body ? { body: JSON.stringify(body) } : {}),
	});
	if (response.status === 404 && !body) return undefined;
	if (!response.ok) throw new Error(`GitHub ${body ? 'PUT' : 'GET'} ${endpoint}: HTTP ${response.status}`);
	return response.json();
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
		if (!directory || !['collect', 'publish'].includes(command)) {
			throw new Error('Usage: release-assets.mjs collect DIRECTORY | publish DIRECTORY VERSION');
		}
		if (command === 'collect') await collectAssets(process.cwd(), resolve(directory));
		else await publishManifests(resolve(directory), version);
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
