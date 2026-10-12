import { execFileSync } from 'node:child_process';
import { closeSync, constants, fstatSync, openSync, readSync, realpathSync } from 'node:fs';
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path';

import type { Environment } from '#runtime';

function originMatches(remote: string, server: string, repository: string): boolean {
	try {
		const scp = /^(?:[^@/]+@)?([^:/]+):(.+)$/.exec(remote);
		const url = new URL(scp && !remote.includes('://') ? `ssh://${scp[1]}/${scp[2]}` : remote);
		const expected = new URL(server);
		return ['https:', 'ssh:'].includes(url.protocol) && url.hostname.toLowerCase() === expected.hostname.toLowerCase()
			&& (url.protocol === 'ssh:' || url.port === expected.port)
			&& url.pathname.replace(/^\//, '').replace(/\.git$/, '').toLowerCase() === repository.toLowerCase();
	} catch {
		return false;
	}
}

// Verify local bytes against the run's commit before linking into its repository.
export function sourceLinkVerifier(environment: Environment): (path: string) => boolean {
	const cache = new Map<string, boolean>();
	let workspace = '';
	let valid: boolean | undefined;
	const limit = 1024 * 1024;
	const deadline = Date.now() + 10_000;
	const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.toUpperCase().startsWith('GIT_')));
	Object.assign(env, {
		GIT_CONFIG_NOSYSTEM: '1',
		// Git accepts /dev/null on Windows; Node's \\.\nul device path is not a valid Git config path.
		GIT_CONFIG_GLOBAL: '/dev/null',
		GIT_TERMINAL_PROMPT: '0',
		GIT_NO_REPLACE_OBJECTS: '1',
		GIT_NO_LAZY_FETCH: '1',
	});
	const git = (directory: string, ...args: string[]): Buffer => {
		const remaining = deadline - Date.now();
		if (remaining <= 0) throw new Error('Source link verification timed out');
		return execFileSync('git', ['-C', directory, ...args], {
			env,
			timeout: Math.min(1500, remaining),
			maxBuffer: limit + 1,
			stdio: ['ignore', 'pipe', 'pipe'],
		});
	};
	return path => {
		if (cache.has(path)) return cache.get(path) || false;
		cache.set(path, false);
		try {
			if (valid === undefined) {
				valid = false;
				workspace = realpathSync(environment.GITHUB_WORKSPACE || '.');
				valid = realpathSync(git(workspace, 'rev-parse', '--show-toplevel').toString().trim()) === workspace
					&& git(workspace, 'rev-parse', 'HEAD').toString().trim() === environment.GITHUB_SHA
					&& originMatches(
						git(workspace, 'config', '--local', '--get', 'remote.origin.url').toString().trim(),
						environment.GITHUB_SERVER_URL || 'https://github.com',
						environment.GITHUB_REPOSITORY || '',
					);
			}
			if (!valid) return false;
			const file = resolve(workspace, path);
			const inside = relative(workspace, file);
			if (!inside || isAbsolute(inside) || inside === '..' || inside.startsWith(`..${sep}`)) return false;
			if (realpathSync(git(dirname(file), 'rev-parse', '--show-toplevel').toString().trim()) !== workspace) {
				return false;
			}
			const handle = openSync(file, constants.O_RDONLY | (constants.O_NOFOLLOW || 0) | constants.O_NONBLOCK);
			try {
				const stat = fstatSync(handle);
				if (!stat.isFile() || stat.size > limit || realpathSync(file) !== file) return false;
				const bytes = Buffer.alloc(limit + 1);
				let length = 0;
				while (length < bytes.length) {
					const count = readSync(handle, bytes, length, bytes.length - length, length);
					if (!count) break;
					length += count;
				}
				if (length > limit) return false;
				const blob = git(workspace, 'cat-file', 'blob', `${environment.GITHUB_SHA}:${path.split(sep).join('/')}`);
				const equal = blob.equals(bytes.subarray(0, length));
				cache.set(path, equal);
				return equal;
			} finally {
				closeSync(handle);
			}
		} catch {
			return false;
		}
	};
}
