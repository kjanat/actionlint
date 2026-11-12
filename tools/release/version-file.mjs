import { randomUUID } from 'node:crypto';
import { readFile, rename, rm, stat, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

/** @typedef {{path: string, original: string, content: string}} FileUpdate */

/** @param {string} version */
export function parseVersion(version) {
	if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
		throw new Error(`Version ${JSON.stringify(version)} must have three dot-separated numbers without leading zeroes`);
	}
	return version.split('.').map(part => BigInt(part));
}

/** @param {string} left @param {string} right */
export function compareVersions(left, right) {
	const other = parseVersion(right);
	for (const [index, part] of parseVersion(left).entries()) {
		if (part < other[index]) return -1;
		if (part > other[index]) return 1;
	}
	return 0;
}

/** @param {string} root */
export async function readVersion(root) {
	const version = (await readFile(join(root, 'VERSION'), 'utf8')).trim();
	parseVersion(version);
	return version;
}

/** @param {string} root @param {FileUpdate[]} updates */
export async function writeUpdates(root, updates) {
	for (const update of updates) {
		if (await readFile(join(root, update.path), 'utf8') !== update.original) {
			throw new Error(`${update.path} changed after version preparation`);
		}
	}
	/** @type {FileUpdate[]} */
	const written = [];
	/** @param {string} path @param {string} content */
	const replace = async (path, content) => {
		const temporary = `${path}.version-${randomUUID()}`;
		const metadata = await stat(path);
		if (!(metadata.mode & 0o222)) throw new Error(`${path} is read-only`);
		try {
			await writeFile(temporary, content, { flag: 'wx', mode: metadata.mode & 0o777 });
			await rename(temporary, path);
		} finally {
			await rm(temporary, { force: true });
		}
	};
	try {
		for (const update of updates) {
			if (update.content === update.original) continue;
			await replace(join(root, update.path), update.content);
			written.push(update);
		}
	} catch (error) {
		const failures = [error];
		for (const update of written.reverse()) {
			try {
				if (await readFile(join(root, update.path), 'utf8') !== update.content) {
					throw new Error(`${update.path} changed during rollback`);
				}
				await replace(join(root, update.path), update.original);
			} catch (restoreError) {
				failures.push(restoreError);
			}
		}
		if (failures.length > 1) {
			throw new AggregateError(failures, 'Version update failed and some files could not be restored');
		}
		throw error;
	}
}
