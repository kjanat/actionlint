import { createHash } from 'node:crypto';
import { lstat } from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, normalize } from 'node:path';

import * as cache from '@actions/cache';

import { sha256File } from '#download';
import { capture } from '#native';
import type { Environment } from '#runtime';
import { InputError } from '#runtime';
import { commandEscape } from '#workflow';

export type PluginPlan = { directory: string; artifacts: string[] };

type PluginProbe = typeof capture;

function unsupportedPluginFlag(stderr: string): boolean {
	const lines = stderr.trimEnd().split(/\r?\n/);
	if (lines.length === 1) {
		return lines[0] === 'unknown flag: --github-action-plugins'
			|| lines[0] === 'flag provided but not defined: -github-action-plugins';
	}
	return lines[0] === 'flag provided but not defined: -github-action-plugins'
		&& /^Usage of .+:$/.test(lines[1] || '')
		&& lines.slice(2).every((line) => line === '' || /^\s+-/.test(line) || /^\s{4,}\S/.test(line));
}

export type PluginCache = {
	restore: (paths: string[], key: string) => Promise<string | undefined>;
	save: (paths: string[], key: string) => Promise<unknown>;
};

const service: PluginCache = {
	restore: (paths, key) => cache.restoreCache(paths, key),
	save: (paths, key) => cache.saveCache(paths, key),
};

export function parsePluginPlan(value: unknown): PluginPlan {
	if (
		typeof value !== 'object' || value === null
		|| !('directory' in value) || typeof value.directory !== 'string'
		|| !('artifacts' in value) || !Array.isArray(value.artifacts)
	) throw new Error('actionlint returned an invalid plugin cache plan');
	const directory = value.directory;
	// Validate native cache locations; check toolkit glob support separately.
	if (
		!isAbsolute(directory) || normalize(directory) !== directory
		|| basename(directory) !== 'artifacts' || basename(dirname(directory)) !== 'actionlint'
	) throw new Error('actionlint returned an unsafe plugin cache directory');
	const artifacts: string[] = [];
	for (const digest of value.artifacts) {
		if (typeof digest !== 'string' || !/^[a-f0-9]{64}$/.test(digest)) {
			throw new Error('actionlint returned an invalid plugin artifact digest');
		}
		artifacts.push(digest);
	}
	return { directory, artifacts: [...new Set(artifacts)].sort() };
}

function cacheable(plan: PluginPlan): boolean {
	return !/[\r\n*?![\]{}]/.test(plan.directory);
}

export function pluginCacheKey(plan: PluginPlan, platform = process.platform): string {
	const digest = createHash('sha256').update([...new Set(plan.artifacts)].sort().join('\n')).digest('hex');
	return `actionlint-plugins-${platform}-${digest}`;
}

export async function inspectPlugins(
	executable: string,
	environment: Environment,
	probe: PluginProbe = capture,
): Promise<PluginPlan | undefined> {
	const result = await probe(executable, ['-github-action-plugins'], environment, { timeoutMS: 300_000 });
	if (result.exitCode !== 0) {
		if (result.exitCode === 2 && unsupportedPluginFlag(result.stderr)) return undefined;
		const message = result.stderr.trim();
		const error = message || `actionlint plugin inspection exited with ${result.exitCode}`;
		throw result.exitCode === 2 ? new InputError(error) : new Error(error);
	}
	return parsePluginPlan(JSON.parse(result.stdout));
}

function warning(operation: string, error: unknown): void {
	const message = error instanceof Error ? error.message : String(error);
	console.log(`::warning::${commandEscape(`Plugin cache ${operation} failed: ${message}`)}`);
}

async function verifiedArtifacts(plan: PluginPlan): Promise<boolean> {
	await checkDirectory(plan.directory);
	let complete = true;
	for (const digest of plan.artifacts) {
		const path = join(plan.directory, digest);
		try {
			const info = await lstat(path);
			if (!info.isFile()) throw new Error(`Plugin cache artifact is not a regular file: ${digest}`);
			if (await sha256File(path) !== digest) throw new Error(`Plugin cache SHA256 mismatch: ${digest}`);
		} catch (error) {
			if (error instanceof Error && 'code' in error && error.code === 'ENOENT') complete = false;
			else throw error;
		}
	}
	return complete;
}

async function checkDirectory(directory: string): Promise<void> {
	try {
		if (!(await lstat(directory)).isDirectory()) throw new Error('Plugin cache path is not a regular directory');
	} catch (error) {
		if (!(error instanceof Error) || !('code' in error) || error.code !== 'ENOENT') throw error;
	}
}

export async function cachePlugins(
	plan: PluginPlan,
	run: () => Promise<number>,
	client: PluginCache,
	inspectAfter?: () => Promise<PluginPlan | undefined>,
): Promise<number> {
	if (!cacheable(plan)) return run();
	const paths = plan.artifacts.map((digest) => join(plan.directory, digest));
	const key = pluginCacheKey(plan);
	let hit: string | undefined;
	if (paths.length > 0) {
		await checkDirectory(plan.directory);
		try {
			hit = await client.restore(paths, key);
		} catch (error) {
			warning('restore', error);
		}
		// A restored archive is an optimization, never evidence of integrity.
		await verifiedArtifacts(plan);
	}
	try {
		return await run();
	} finally {
		// Automatic config migration can add pins during analysis. Inspect again
		// so its first run saves the newly verified default artifacts too.
		let current = plan;
		let canSave = true;
		if (inspectAfter) {
			try {
				const refreshed = await inspectAfter();
				if (refreshed) current = refreshed;
				else canSave = false;
			} catch (error) {
				warning('plan refresh', error);
			}
		}
		const currentKey = pluginCacheKey(current);
		if (
			canSave && cacheable(current) && current.artifacts.length > 0
			&& hit !== currentKey && await verifiedArtifacts(current)
		) {
			try {
				await client.save(current.artifacts.map((digest) => join(current.directory, digest)), currentKey);
			} catch (error) {
				warning('save', error);
			}
		}
	}
}

export async function withPluginCache(
	executable: string,
	environment: Environment,
	run: () => Promise<number>,
	probe: PluginProbe = capture,
	client: PluginCache = service,
): Promise<number> {
	const plan = await inspectPlugins(executable, environment, probe);
	if (!plan) return run();
	return cachePlugins(plan, run, client, () => inspectPlugins(executable, environment, probe));
}
