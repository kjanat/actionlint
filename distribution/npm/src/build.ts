import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { cp, lstat, mkdir, readFile, writeFile } from 'node:fs/promises';
import { isAbsolute, join, relative, resolve, sep } from 'node:path';
import { setTimeout } from 'node:timers/promises';

import { archiveMembers, readBounded } from '#archives';
import type { Target, Targets } from '#targets';
import { jsonObject, loadTargets, packageName, selectTargets } from '#targets';

export interface BuildOptions {
	version: string;
	repository: string;
	token: string;
	repoRoot: string;
	npmDir: string;
	outDir: string;
	downloads: string;
	only: string;
}

export interface BuildServices {
	fetch?: typeof fetch;
	report?: (message: string) => void;
}

export function releaseVersion(input: string): string {
	const version = input.replace(/^v/, '');
	if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
		throw new Error(`Version ${JSON.stringify(input)} is not X.Y.Z or X.Y.Z-prerelease`);
	}
	return version;
}

export function assetName(version: string, target: Target): string {
	return `actionlint_${version}_${target.asset}.${target.format}`;
}

export function parseChecksums(content: string): Map<string, string> {
	const checksums = new Map<string, string>();
	for (const line of content.split(/\r?\n/)) {
		if (!line.trim()) continue;
		const match = /^([a-fA-F0-9]{64})\s+\*?(\S+)$/.exec(line.trim());
		const digest = match?.[1];
		const name = match?.[2];
		if (!digest || !name) throw new Error(`Malformed release checksum line: ${line}`);
		if (checksums.has(name)) throw new Error(`Release checksums list ${name} twice`);
		checksums.set(name, digest.toLowerCase());
	}
	if (!checksums.size) throw new Error('The release manifest listed no checksums');
	return checksums;
}

async function responseBytes(response: Response, name: string): Promise<Buffer> {
	if (!response.body) throw new Error(`${name} has no response body`);
	const reader = response.body.getReader();
	async function* chunks() {
		try {
			for (;;) {
				const chunk = await reader.read();
				if (chunk.done) return;
				yield chunk.value;
			}
		} finally {
			await reader.cancel();
			reader.releaseLock();
		}
	}
	return readBounded(chunks(), name);
}

async function readAsset(options: BuildOptions, name: string, request: typeof fetch): Promise<Buffer> {
	if (options.downloads) {
		try {
			return await readBounded(createReadStream(join(options.downloads, name)), name);
		} catch (error) {
			if (!(error instanceof Error && 'code' in error && error.code === 'ENOENT')) throw error;
		}
	}
	if (!options.repository) {
		throw new Error(`${name} is not in ${options.downloads || 'a downloads directory'} and no repository was supplied`);
	}
	if (!/^[\w.-]+\/[\w.-]+$/.test(options.repository)) throw new Error('Repository must be owner/name');
	const url = `https://github.com/${options.repository}/releases/download/v${options.version}/${name}`;
	let failure: unknown;
	for (let attempt = 0; attempt < 4; attempt++) {
		if (attempt) await setTimeout((2 ** attempt) * 1000);
		try {
			const response = await request(url, {
				headers: options.token ? { Authorization: `Bearer ${options.token}` } : {},
				signal: AbortSignal.timeout(30_000),
			});
			if (!response.ok) {
				await response.body?.cancel();
				throw new Error(`GET ${url}: HTTP ${response.status}`);
			}
			return await responseBytes(response, name);
		} catch (error) {
			failure = error;
		}
	}
	throw new Error(`Could not download ${name}`, { cause: failure });
}

async function verifiedArchive(
	options: BuildOptions,
	target: Target,
	sums: ReadonlyMap<string, string>,
	request: typeof fetch,
): Promise<Buffer> {
	const name = assetName(options.version, target);
	const expected = sums.get(name);
	if (!expected) throw new Error(`The checksums manifest lists no digest for ${name}`);
	const archive = await readAsset(options, name, request);
	const actual = createHash('sha256').update(archive).digest('hex');
	if (actual !== expected) throw new Error(`${name} digest mismatch: manifest says ${expected}, downloaded ${actual}`);
	return archive;
}

async function writeJson(path: string, value: Record<string, unknown>): Promise<void> {
	await writeFile(path, `${JSON.stringify(value, null, 2)}\n`);
}

function platformManifest(
	targets: Targets,
	target: Target,
	version: string,
	facade: Record<string, unknown>,
): Record<string, unknown> {
	const manifest: Record<string, unknown> = {
		name: packageName(targets, target),
		version,
		description: `The ${target.os}-${target.cpu} binary for ${targets.facade}.`,
	};
	for (const key of ['homepage', 'bugs', 'repository', 'funding', 'license', 'author']) {
		if (Object.hasOwn(facade, key)) manifest[key] = facade[key];
	}
	return {
		...manifest,
		os: [target.os],
		cpu: [target.cpu],
		files: ['bin/'],
		exports: { './package.json': './package.json' },
		preferUnplugged: true,
	};
}

async function copyLegal(root: string, destination: string): Promise<void> {
	await cp(join(root, 'LICENSE.txt'), join(destination, 'LICENSE.txt'));
}

async function copyDirectory(source: string, destination: string): Promise<void> {
	await cp(source, destination, {
		recursive: true,
		filter: async path => {
			const entry = await lstat(path);
			return entry.isFile() || entry.isDirectory();
		},
	});
}

function ensureSeparateOutput(source: string, destination: string): void {
	const path = relative(resolve(source), resolve(destination));
	if (path !== '..' && !path.startsWith(`..${sep}`) && !isAbsolute(path)) {
		throw new Error(`Output ${destination} must be outside the facade sources`);
	}
}

export async function buildPackages(input: BuildOptions, services: BuildServices = {}): Promise<string[]> {
	const options = { ...input, version: releaseVersion(input.version) };
	if (!options.npmDir || !options.outDir || !options.repoRoot) {
		throw new Error('Repository, npm source and output directories are required');
	}
	ensureSeparateOutput(join(options.npmDir, 'facade'), join(options.outDir, 'facade'));
	const targets = await loadTargets(join(options.npmDir, 'targets.json'));
	const selected = selectTargets(targets, options.only);
	const raw: unknown = JSON.parse(await readFile(join(options.npmDir, 'facade/package.json'), 'utf8'));
	const facade = jsonObject(raw, 'facade package.json');
	const request = services.fetch ?? fetch;
	const report = services.report ?? console.log;
	const checksums = parseChecksums(
		(await readAsset(options, `actionlint_${options.version}_checksums.txt`, request)).toString('utf8'),
	);
	const packages: string[] = [];
	let manual: Buffer | undefined;
	await mkdir(options.outDir, { recursive: true });
	for (const target of selected) {
		const executable = targets.binary + (target.os === 'win32' ? '.exe' : '');
		const members = await archiveMembers(await verifiedArchive(options, target, checksums, request), target.format, [
			executable,
			`${targets.binary}.1`,
		]);
		const binary = members.get(executable);
		manual ??= members.get(`${targets.binary}.1`);
		if (!binary || !manual) throw new Error(`Incomplete release archive for ${target.pkg}`);
		const directory = join(options.outDir, target.pkg);
		await mkdir(join(directory, 'bin'), { recursive: true });
		await writeFile(join(directory, 'bin', executable), binary, { mode: 0o755 });
		await writeJson(join(directory, 'package.json'), platformManifest(targets, target, options.version, facade));
		await copyLegal(options.repoRoot, directory);
		const name = packageName(targets, target);
		await writeFile(
			join(directory, 'README.md'),
			`# ${name}\n\nThe ${target.os}-${target.cpu} binary for [${targets.facade}](https://www.npmjs.com/package/${targets.facade}).\n\nThe facade installs this package automatically for matching hosts.\n`,
		);
		packages.push(directory);
		report(`built ${name}@${options.version} (${executable}, ${binary.length} bytes)`);
	}
	if (!options.only || options.only === 'facade') {
		if (!manual) {
			const target = targets.targets[0];
			if (!target) throw new Error('No release archive supplies the manual');
			manual = (await archiveMembers(await verifiedArchive(options, target, checksums, request), target.format, [
				`${targets.binary}.1`,
			])).get(`${targets.binary}.1`);
		}
		if (!manual) throw new Error('No release archive supplies the manual');
		const directory = join(options.outDir, 'facade');
		await mkdir(join(directory, 'man'), { recursive: true });
		for (const part of ['bin', 'lib', 'types']) {
			await copyDirectory(join(options.npmDir, 'facade', part), join(directory, part));
		}
		await cp(join(options.npmDir, 'facade/README.md'), join(directory, 'README.md'));
		await cp(join(options.repoRoot, 'actionlint.schema.json'), join(directory, 'actionlint.schema.json'));
		await copyDirectory(join(options.repoRoot, 'schemas'), join(directory, 'schemas'));
		await copyLegal(options.repoRoot, directory);
		await writeFile(join(directory, 'man', `${targets.binary}.1`), manual);
		facade.version = options.version;
		facade.optionalDependencies = Object.fromEntries(
			targets.targets.map(target => [packageName(targets, target), options.version]),
		);
		await writeJson(join(directory, 'package.json'), facade);
		packages.push(directory);
		report(`built ${targets.facade}@${options.version} with ${targets.targets.length} platform packages`);
	}
	return packages;
}
