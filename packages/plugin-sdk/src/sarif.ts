import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import type { Diagnostic, Severity } from './index.ts';
import { array, positiveInteger, record, text } from './process.ts';

/**
 * Decode SARIF 2.1.0 results, accepting only artifact URIs allowed by the caller.
 * Columns and rows are retained as reported; the host validates the resulting ranges.
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
			if (region.endLine !== undefined || region.endColumn !== undefined) {
				if (region.endLine === undefined || region.endColumn === undefined) {
					throw new Error('SARIF region has an incomplete end position');
				}
				diagnostic.end_location = {
					row: positiveInteger(region.endLine, 'SARIF end line'),
					column: positiveInteger(region.endColumn, 'SARIF end column'),
				};
			}
			diagnostics.push(diagnostic);
		}
	}
	return diagnostics;
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
