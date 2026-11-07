import { readFile } from 'node:fs/promises';
import { basename, resolve } from 'node:path';

import type { Environment } from '#runtime';
import { InputError } from '#runtime';

export function releaseVersion(value: string): string {
	const version = value.trim().replace(/^v/, '');
	const prerelease = version.split('+')[0]?.match(/-(.+)$/)?.[1];
	if (
		prerelease?.split('.').some((identifier) => /^0\d+$/.test(identifier))
		|| !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*)?(?:\+[\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*)?$/
			.test(version)
	) {
		throw new InputError(`Expected an exact actionlint release version, such as 1.17.0; got ${JSON.stringify(value)}`);
	}
	return version;
}

export function versionFromFile(content: string, path: string): string {
	const lines = content.split(/\r?\n/).map((line) => line.split('#')[0]?.trim() ?? '').filter(Boolean);
	const entries = lines.map((line) => line.split(/\s+/)).filter(([tool]) => tool === 'actionlint');
	if (basename(path) === '.tool-versions' || entries.length > 0) {
		const [entry] = entries;
		if (entries.length !== 1 || entry?.length !== 2 || !entry[1]) {
			throw new InputError(`${path} must contain one 'actionlint <version>' entry with one exact release version`);
		}
		return releaseVersion(entry[1]);
	}
	return releaseVersion(lines.join('\n'));
}

export async function selectedVersion(environment: Environment, bundled: string): Promise<string> {
	const explicit = environment.INPUT_VERSION?.trim();
	if (explicit) return releaseVersion(explicit);
	const file = environment['INPUT_VERSION-FILE']?.trim();
	if (!file) return releaseVersion(bundled);
	const path = resolve(environment.GITHUB_WORKSPACE || '.', environment['INPUT_WORKING-DIRECTORY'] || '.', file);
	let content: string;
	try {
		content = await readFile(path, 'utf8');
	} catch (error) {
		throw new InputError(`Cannot read version-file ${path}: ${error instanceof Error ? error.message : String(error)}`);
	}
	return versionFromFile(content, path);
}
