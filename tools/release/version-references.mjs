import { randomUUID } from 'node:crypto';
import { readFile, rename, rm, stat, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

/** @typedef {{description: string, pattern: RegExp, count: number}} VersionRule */
/** @typedef {{path: string, rules: VersionRule[], unrelated?: string[], generated?: RegExp[]}} VersionTarget */
/** @typedef {{description: string, start: number, end: number, valueStart: number, valueEnd: number, version: string}} Occurrence */
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

/** @param {string} content @param {number} position */
function lineAt(content, position) {
	return content.slice(0, position).split('\n').length;
}

/** @param {VersionTarget} target @param {string} content @returns {Occurrence[]} */
export function scanReferences(target, content) {
	/** @type {Occurrence[]} */
	const occurrences = [];
	/** @type {{start: number, end: number}[]} */
	const covered = [];
	for (const rule of target.rules) {
		if (!Number.isSafeInteger(rule.count) || rule.count < 1) {
			throw new Error(`${target.path}: invalid count for ${rule.description}`);
		}
		const matches = [...content.matchAll(rule.pattern)];
		if (matches.length !== rule.count) {
			throw new Error(
				`${target.path}: expected ${rule.count} occurrence(s) of ${rule.description}, found ${matches.length}`,
			);
		}
		for (const match of matches) {
			const span = match.indices?.[1];
			if (!span || match.length !== 2) {
				throw new Error(`${target.path}: ${rule.description} must capture exactly one version`);
			}
			const version = match[1];
			if (version !== 'nightly') parseVersion(version);
			const occurrence = {
				description: rule.description,
				start: match.index,
				end: match.index + match[0].length,
				valueStart: span[0],
				valueEnd: span[1],
				version,
			};
			occurrences.push(occurrence);
			covered.push(occurrence);
		}
	}
	occurrences.sort((a, b) => a.start - b.start);
	for (let index = 1; index < occurrences.length; index++) {
		const current = occurrences[index];
		const previous = occurrences[index - 1];
		if (current.start < previous.end) {
			throw new Error(
				`${target.path}:${lineAt(content, current.start)}: ${previous.description} and ${current.description} overlap`,
			);
		}
	}
	for (const literal of target.unrelated ?? []) {
		if (!literal || !content.includes(literal)) {
			throw new Error(`${target.path}: unrelated version reference ${JSON.stringify(literal)} is absent`);
		}
		for (let start = content.indexOf(literal); start !== -1; start = content.indexOf(literal, start + literal.length)) {
			covered.push({ start, end: start + literal.length });
		}
	}
	for (const pattern of target.generated ?? []) {
		const matches = [...content.matchAll(pattern)];
		if (!matches.length) throw new Error(`${target.path}: generated-version pattern ${pattern} no longer matches`);
		covered.push(...matches.map(match => ({ start: match.index, end: match.index + match[0].length })));
	}
	for (const match of content.matchAll(/\d+\.\d+\.\d+/g)) {
		if (!covered.some(span => span.start <= match.index && match.index + match[0].length <= span.end)) {
			throw new Error(
				`${target.path}:${lineAt(content, match.index)}: undeclared version reference ${
					JSON.stringify(match[0])
				}; declare it in tools/release/version-targets.mjs`,
			);
		}
	}
	return occurrences;
}

/** @param {VersionTarget} target @param {string} content @param {string} version */
export function rewriteReferences(target, content, version) {
	parseVersion(version);
	let output = '';
	let end = 0;
	for (const occurrence of scanReferences(target, content)) {
		if (occurrence.version === 'nightly') continue;
		if (compareVersions(occurrence.version, version) > 0) {
			throw new Error(
				`${target.path}:${
					lineAt(content, occurrence.valueStart)
				}: ${occurrence.description} refers to ${occurrence.version}, which is newer than ${version}`,
			);
		}
		output += content.slice(end, occurrence.valueStart) + version;
		end = occurrence.valueEnd;
	}
	output += content.slice(end);
	for (const occurrence of scanReferences(target, output)) {
		if (occurrence.version !== 'nightly' && occurrence.version !== version) {
			throw new Error(`${target.path}: version rewrite failed`);
		}
	}
	return output;
}

/** @param {string} root @param {VersionTarget[]} targets @param {(line: string) => void} [report] */
export async function checkReferences(root, targets, report = () => {}) {
	/** @type {Map<string, number>} */
	const versions = new Map();
	for (const target of targets) {
		const content = await readFile(join(root, target.path), 'utf8');
		for (const occurrence of scanReferences(target, content)) {
			report(
				`${target.path}:${lineAt(content, occurrence.valueStart)}: ${occurrence.description}: ${occurrence.version}${
					occurrence.version === 'nightly' ? ' (development)' : ''
				}`,
			);
			if (occurrence.version !== 'nightly') {
				versions.set(occurrence.version, (versions.get(occurrence.version) ?? 0) + 1);
			}
		}
	}
	for (const [version, count] of [...versions].sort(([a], [b]) => compareVersions(a, b))) {
		report(`version ${version} is referenced ${count} time(s)`);
	}
	if (versions.size !== 1) {
		throw new Error(
			versions.size
				? `Release version references disagree: ${[...versions.keys()].join(', ')}`
				: 'No declared file references a release version',
		);
	}
	return [...versions.keys()][0];
}

/** @param {string} root @param {VersionTarget[]} targets @param {string} version @returns {Promise<FileUpdate[]>} */
export async function planVersionUpdates(root, targets, version) {
	parseVersion(version);
	const updates = [];
	for (const target of targets) {
		const original = await readFile(join(root, target.path), 'utf8');
		updates.push({ path: target.path, original, content: rewriteReferences(target, original, version) });
	}
	return updates;
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
