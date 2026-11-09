import { isAlias, isMap, isScalar, isSeq, LineCounter, type Node, parseDocument } from 'yaml';

import { unicodeColumn } from '#index';

export { diagnosticAtImage, imageKey } from '#image-report';

export interface ImageReference {
	value: string;
	start: { row: number; column: number };
	end: { row: number; column: number };
}

/**
 * Extract static OCI references from workflow containers/services, docker actions, and action images.
 * Dynamic expressions, aliases, malformed YAML, and unsupported references throw descriptive errors.
 * @example
 * ```ts
 * import { diagnosticAtImage, imageReferences } from '@kjanat-actionlint/plugin-sdk/images';
 * const images = imageReferences(request.source, request.kind);
 * const diagnostics = images.map((image) => diagnosticAtImage(
 *   request.filename, image, 'image-policy', `Review ${image.value}`, 'warning',
 * ));
 * ```
 */
export function imageReferences(source: string, kind: 'workflow' | 'action'): ImageReference[] {
	const lineCounter = new LineCounter();
	const document = parseDocument(source, { lineCounter, keepSourceTokens: true, uniqueKeys: true });
	if (document.errors.length > 0) {
		throw new Error(`captured YAML is invalid: ${document.errors.map((error) => error.message).join('; ')}`);
	}
	const result: ImageReference[] = [];
	const add = (node: Node | null, label: string, prefix = '') => {
		if (node === null) return;
		if (isAlias(node)) throw new Error(`${label} uses a YAML alias; image target is unresolved`);
		if (!isScalar(node) || typeof node.value !== 'string') {
			throw new Error(`${label} must be a static image reference string`);
		}
		const raw = node.value;
		if (raw.includes('${{') || raw.includes('$(')) {
			throw new Error(`${label} uses a dynamic image reference; image target is unresolved`);
		}
		if (!raw.startsWith(prefix)) throw new Error(`${label} does not use the expected ${prefix} prefix`);
		const value = node.type === 'BLOCK_LITERAL' || node.type === 'BLOCK_FOLDED'
			? raw.slice(prefix.length).replace(/\n$/u, '')
			: raw.slice(prefix.length);
		if (!validImageReference(value)) throw new Error(`${label} is not a supported static OCI image reference`);
		const range = node.range;
		if (range === undefined || range === null) throw new Error(`${label} has no YAML source range`);
		const startOffset = range[0];
		const endOffset = range[1];
		if (startOffset >= endOffset) throw new Error(`${label} has an empty image reference`);
		result.push({
			value,
			start: sourcePosition(source, lineCounter, startOffset),
			end: sourcePosition(source, lineCounter, endOffset),
		});
	};

	const root = document.contents;
	if (kind === 'action') {
		const runs = mapValue(root, 'runs');
		const image = mapValue(runs, 'image');
		if (image !== null && isScalar(image) && typeof image.value === 'string' && image.value.startsWith('docker://')) {
			add(image, 'runs.image', 'docker://');
		}
		addDockerSteps(mapValue(runs, 'steps'), 'runs.steps', add);
	}
	if (kind === 'workflow') {
		const jobs = mapValue(root, 'jobs');
		for (const { key: jobName, value: job } of mapEntries(jobs, 'jobs')) {
			const container = mapValue(job, 'container');
			if (isMap(container)) add(mapValue(container, 'image'), `jobs.${jobName}.container.image`);
			else add(container, `jobs.${jobName}.container`);
			for (
				const { key: serviceName, value: service } of mapEntries(mapValue(job, 'services'), `jobs.${jobName}.services`)
			) {
				add(mapValue(service, 'image'), `jobs.${jobName}.services.${serviceName}.image`);
			}
			addDockerSteps(mapValue(job, 'steps'), `jobs.${jobName}.steps`, add);
		}
	}
	return result;
}

function addDockerSteps(
	node: Node | null,
	label: string,
	add: (node: Node | null, label: string, prefix?: string) => void,
): void {
	for (const [index, step] of sequenceEntries(node, label).entries()) {
		const uses = mapValue(step, 'uses');
		if (uses !== null && isScalar(uses) && typeof uses.value === 'string' && uses.value.startsWith('docker://')) {
			add(uses, `${label}[${index}].uses`, 'docker://');
		}
	}
}

function mapEntries(node: Node | null, label: string): { key: string; value: Node | null }[] {
	if (node === null) return [];
	if (!isMap(node)) throw new Error(`${label} must be a mapping`);
	const entries: { key: string; value: Node | null }[] = [];
	const seen = new Set<string>();
	for (const pair of node.items) {
		if (!isScalar(pair.key) || typeof pair.key.value !== 'string') continue;
		const key = pair.key.value;
		if (seen.has(key)) throw new Error(`${label} has duplicate key ${key}`);
		seen.add(key);
		entries.push({ key, value: nodeValue(pair.value, `${label}.${key}`) });
	}
	return entries;
}

function sequenceEntries(node: Node | null, label: string): (Node | null)[] {
	if (node === null) return [];
	if (!isSeq(node)) throw new Error(`${label} must be a sequence`);
	return node.items.map((item, index) => nodeValue(item, `${label}[${index}]`));
}

function nodeValue(value: unknown, label: string): Node | null {
	if (value === null) return null;
	if (isAlias(value) || isScalar(value) || isMap(value) || isSeq(value)) return value;
	throw new Error(`${label} is not a YAML node`);
}

function mapValue(node: Node | null, key: string): Node | null {
	return mapEntries(node, key).find((entry) => entry.key === key)?.value ?? null;
}

function sourcePosition(
	source: string,
	lineCounter: LineCounter,
	offset: number,
): { row: number; column: number } {
	const position = lineCounter.linePos(offset);
	if (position.line < 1) throw new Error('YAML parser returned a missing line position');
	const lineStart = lineCounter.lineStarts[position.line - 1];
	if (lineStart === undefined) throw new Error('YAML parser returned an invalid line position');
	const nextLine = lineCounter.lineStarts[position.line];
	const lineEnd = nextLine === undefined ? source.length : nextLine;
	const line = source.slice(lineStart, lineEnd).replace(/\r?\n$/u, '');
	return { row: position.line, column: unicodeColumn(line, position.col) };
}

function validImageReference(value: string): boolean {
	if (value.length === 0 || value.length > 2048 || value !== value.trim()) return false;
	if (/\s/u.test(value) || value.startsWith('-')) return false;
	const at = value.indexOf('@');
	if (at !== -1 && !/^sha256:[a-f0-9]{64}$/u.test(value.slice(at + 1))) return false;
	const nameAndTag = at === -1 ? value : value.slice(0, at);
	const colon = nameAndTag.lastIndexOf(':');
	const name = colon > nameAndTag.lastIndexOf('/') ? nameAndTag.slice(0, colon) : nameAndTag;
	const tag = colon > nameAndTag.lastIndexOf('/') ? nameAndTag.slice(colon + 1) : undefined;
	if (tag !== undefined && !/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/u.test(tag)) return false;
	const parts = name.split('/');
	if (parts.some((part) => part.length === 0)) return false;
	const first = parts[0];
	if (first === undefined) return false;
	const hasRegistry = parts.length > 1 && (first.includes('.') || first.includes(':') || first === 'localhost');
	const repository = hasRegistry ? parts.slice(1) : parts;
	if (hasRegistry && !validRegistry(first)) return false;
	return repository.every((part) => /^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$/u.test(part));
}

function validRegistry(value: string): boolean {
	const colon = value.lastIndexOf(':');
	const hostname = colon < 0 ? value : value.slice(0, colon);
	if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/u.test(hostname)) return false;
	if (colon < 0) return true;
	const port = Number(value.slice(colon + 1));
	return Number.isInteger(port) && port >= 1 && port <= 65535;
}
