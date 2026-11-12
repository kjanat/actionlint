import { readFile } from 'node:fs/promises';

export type ArchiveFormat = 'tar.gz' | 'zip';

export interface Target {
	pkg: string;
	os: string;
	cpu: string;
	asset: string;
	format: ArchiveFormat;
}

export interface Targets {
	facade: string;
	platformScope: string;
	binary: string;
	targets: Target[];
}

export function jsonObject(value: unknown, description: string): Record<string, unknown> {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) {
		throw new Error(`${description} must be a JSON object`);
	}
	return Object.fromEntries(Object.entries(value));
}

function text(record: Record<string, unknown>, key: string, pattern: RegExp): string {
	const value = record[key];
	if (typeof value !== 'string' || !pattern.test(value)) throw new Error(`Invalid ${key} in npm targets`);
	return value;
}

export function parseTargets(value: unknown): Targets {
	const object = jsonObject(value, 'npm targets');
	const facade = text(object, 'facade', /^@?[a-z0-9][a-z0-9._/-]*$/);
	const platformScope = text(object, 'platformScope', /^@[a-z0-9][a-z0-9._-]*$/);
	const binary = text(object, 'binary', /^[a-z0-9][a-z0-9._-]*$/);
	if (!Array.isArray(object.targets) || object.targets.length === 0) {
		throw new Error('npm targets must list at least one platform');
	}
	const names = new Set<string>();
	const targets = object.targets.map((value: unknown): Target => {
		const target = jsonObject(value, 'npm target');
		const pkg = text(target, 'pkg', /^[a-z0-9]+-[a-z0-9]+$/);
		const os = text(target, 'os', /^[a-z0-9]+$/);
		const cpu = text(target, 'cpu', /^[a-z0-9]+$/);
		const asset = text(target, 'asset', /^[a-z0-9]+_[a-z0-9]+$/);
		if (pkg !== `${os}-${cpu}`) throw new Error(`Target ${pkg} must be named ${os}-${cpu} to match os and cpu`);
		if (names.has(pkg)) throw new Error(`npm target ${pkg} is declared twice`);
		names.add(pkg);
		const format = target.format;
		if (format !== 'tar.gz' && format !== 'zip') throw new Error(`Target ${pkg} has unsupported archive format`);
		return { pkg, os, cpu, asset, format };
	});
	return { facade, platformScope, binary, targets };
}

export async function loadTargets(path: string): Promise<Targets> {
	const value: unknown = JSON.parse(await readFile(path, 'utf8'));
	return parseTargets(value);
}

export function packageName(targets: Targets, target: Target): string {
	return `${targets.platformScope}/${targets.binary}-${target.pkg}`;
}

export function selectTargets(targets: Targets, only: string): Target[] {
	if (!only) return targets.targets;
	if (only === 'facade') return [];
	const found = targets.targets.find(target => target.pkg === only || packageName(targets, target) === only);
	if (!found) throw new Error(`No npm target matches ${JSON.stringify(only)}`);
	return [found];
}
