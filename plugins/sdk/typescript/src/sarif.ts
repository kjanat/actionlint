import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import type { Diagnostic, Severity } from './index.ts';
import { array, positiveInteger, record, text } from './process.ts';

/**
 * Decode SARIF 2.1.0 results, accepting only artifact URIs allowed by the caller.
 * Return columns and rows as reported. actionlint validates the resulting ranges.
 * Pass source to calculate line ends when SARIF omits endColumn.
 * Native suppressions with no review status or an accepted status are honored.
 * Report findings with rejected or under-review suppressions. Malformed suppression records throw.
 * @example
 * ```ts
 * import { exactStagedUri, parseSarif } from '@kjanat-actionlint/plugin-sdk/sarif';
 * const diagnostics = parseSarif(result.stdout, request.filename, exactStagedUri(directory, file));
 * ```
 */
export function parseSarif(
	input: string,
	filename: string,
	allowedUris: (uri: string) => boolean,
	source?: string,
): Diagnostic[] {
	let value: unknown;
	try {
		value = JSON.parse(input);
	} catch (error) {
		throw new Error(`scanner returned invalid SARIF: ${messageOf(error)}`);
	}
	const document = record(value, 'SARIF document');
	if (document.version !== '2.1.0') throw new Error('scanner returned unsupported SARIF version');
	const diagnostics: Diagnostic[] = [];
	for (const runValue of array(document.runs, 'SARIF runs')) {
		const run = record(runValue, 'SARIF run');
		for (const resultValue of array(run.results, 'SARIF results')) {
			const result = record(resultValue, 'SARIF result');
			const suppressed = nativeSuppression(result.suppressions);
			const kind = result.kind ?? '';
			if (typeof kind !== 'string') throw new Error('SARIF result kind must be a string');
			if (suppressed || kind !== '' && kind !== 'fail') continue;
			const locationValue = array(result.locations, 'SARIF result locations')[0];
			if (locationValue === undefined) throw new Error('SARIF finding has no primary location');
			const location = record(locationValue, 'SARIF primary location');
			const physical = record(location.physicalLocation, 'SARIF physical location');
			const artifact = record(physical.artifactLocation, 'SARIF artifact location');
			const uri = text(artifact.uri, 'SARIF artifact URI');
			if (!allowedUris(uri)) throw new Error(`SARIF finding points outside the captured document: ${uri}`);
			const region = record(physical.region, 'SARIF region');
			const start = {
				row: positiveInteger(region.startLine, 'SARIF start line'),
				column: region.startColumn === undefined
					? 1
					: positiveInteger(region.startColumn, 'SARIF start column'),
			};
			const message = record(result.message, 'SARIF message');
			const diagnostic: Diagnostic = {
				filename,
				code: text(result.ruleId, 'SARIF rule id'),
				message: text(message.text, 'SARIF message text'),
				severity: severityOf(result.level),
				location: start,
			};
			const endLine = region.endLine === undefined ? start.row : positiveInteger(region.endLine, 'SARIF end line');
			const lastLine = source?.split(/\r\n|\n|\r/u)[endLine - 1];
			const endColumn = region.endColumn === undefined
				? lastLine === undefined ? undefined : [...lastLine].length + 1
				: positiveInteger(region.endColumn, 'SARIF end column');
			if (endLine < start.row || endColumn !== undefined && endLine === start.row && endColumn < start.column) {
				throw new Error('SARIF region ends before its start');
			}
			if (endColumn !== undefined) {
				diagnostic.end_location = {
					row: endLine,
					column: endColumn,
				};
			}
			diagnostics.push(diagnostic);
		}
	}
	return diagnostics;
}

function nativeSuppression(value: unknown): boolean {
	if (value == null) return false;
	let suppressed = false;
	for (const entry of array(value, 'SARIF suppressions')) {
		const suppression = record(entry, 'SARIF suppression');
		if (suppression.kind !== 'inSource' && suppression.kind !== 'external') {
			throw new Error(`invalid SARIF suppression kind ${String(suppression.kind)}`);
		}
		switch (suppression.status) {
			case undefined:
			case null:
			case 'accepted':
				suppressed = true;
				break;
			case 'underReview':
			case 'rejected':
				break;
			default:
				throw new Error(`invalid SARIF suppression status ${String(suppression.status)}`);
		}
	}
	return suppressed;
}

/** Create an allowlist predicate for exactly one path inside a captured directory. */
export function exactStagedUri(directory: string, relativeFile: string): (uri: string) => boolean {
	const expected = resolve(directory, relativeFile);
	return (uri) => {
		if (uri.startsWith('<') && uri.endsWith('>')) return false;
		try {
			const candidate = uri.startsWith('file:') ? fileURLToPath(uri) : uri;
			return resolve(directory, decodeURIComponent(candidate)) === expected;
		} catch {
			return false;
		}
	};
}

function severityOf(value: unknown): Severity {
	if (value === 'error') return 'error';
	if (value === 'warning') return 'warning';
	if (value === 'note' || value === 'none') return 'info';
	throw new Error(`SARIF result has invalid severity ${String(value)}`);
}

function messageOf(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
