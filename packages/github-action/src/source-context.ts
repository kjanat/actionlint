import { constants } from 'node:fs';
import { open, realpath } from 'node:fs/promises';
import { isAbsolute, relative, resolve, sep } from 'node:path';

import type { ActionResult, Diagnostic } from '#result';
import type { Environment } from '#runtime';

function contained(root: string, path: string): boolean {
	const child = relative(root, path);
	return child !== '' && child !== '..' && !child.startsWith(`..${sep}`) && !isAbsolute(child);
}

// Context is optional. Read only regular workspace files, with a fixed byte limit.
export async function sourceContexts(
	result: ActionResult,
	environment: Environment,
): Promise<Map<Diagnostic, string[]>> {
	const contexts = new Map<Diagnostic, string[]>();
	const limit = 1024 * 1024;
	let workspace: string;
	try {
		workspace = await realpath(environment.GITHUB_WORKSPACE || '.');
	} catch {
		return contexts;
	}
	const directory = resolve(environment.GITHUB_WORKSPACE || '.', environment['INPUT_WORKING-DIRECTORY'] || '.');
	const cache = new Map<string, string[]>();
	for (const diagnostic of result.diagnostics.slice(0, 50)) {
		try {
			const path = await realpath(resolve(directory, diagnostic.path));
			if (!contained(workspace, path)) continue;
			let lines = cache.get(path);
			if (!lines) {
				const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW || 0) | constants.O_NONBLOCK);
				try {
					const stat = await file.stat();
					if (!stat.isFile() || stat.size > limit || !contained(workspace, await realpath(path))) continue;
					const buffer = Buffer.alloc(limit + 1);
					let length = 0;
					while (length < buffer.length) {
						const { bytesRead } = await file.read(buffer, length, buffer.length - length, length);
						if (!bytesRead) break;
						length += bytesRead;
					}
					if (length > limit) continue;
					lines = buffer.subarray(0, length).toString('utf8').split(/\r\n|\n|\r/);
					cache.set(path, lines);
				} finally {
					await file.close();
				}
			}
			// Avoid showing stale source beside an analysis-time diagnostic.
			if (
				diagnostic.snippet !== undefined && lines[diagnostic.start.line - 1] !== diagnostic.snippet.replace(/\r$/, '')
			) continue;
			if (lines[diagnostic.start.line - 1] !== undefined) contexts.set(diagnostic, lines);
		} catch {
			// Missing, unreadable and external sources retain the diagnostic's snippet.
		}
	}
	return contexts;
}
