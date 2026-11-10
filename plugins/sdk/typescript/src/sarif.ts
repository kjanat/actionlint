import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import type { Diagnostic, Severity } from '#index';
import { array, positiveInteger, record, text } from '#process';

/**
 * Decode SARIF 2.1.0 results, accepting only artifact URIs allowed by the caller.
 * Pass captured source to convert UTF-16 columns and calculate omitted end columns.
 * Runs without columnKind require source when a column could depend on its unit.
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
	if (document.inlineExternalProperties !== undefined) {
		throw new Error('SARIF inline external properties are unsupported');
	}
	const diagnostics: Diagnostic[] = [];
	for (const runValue of array(document.runs, 'SARIF runs')) {
		const run = record(runValue, 'SARIF run');
		if (run.externalPropertyFileReferences !== undefined) {
			throw new Error('SARIF external property files are unsupported');
		}
		if (run.newlineSequences !== undefined) {
			const sequences = array(run.newlineSequences, 'SARIF newline sequences');
			if (sequences.length !== 2 || sequences[0] !== '\r\n' || sequences[1] !== '\n') {
				throw new Error('SARIF custom newline sequences are unsupported');
			}
		}
		const columnKind = run.columnKind === undefined ? 'unspecified' : run.columnKind;
		if (columnKind !== 'unicodeCodePoints' && columnKind !== 'utf16CodeUnits' && columnKind !== 'unspecified') {
			throw new Error(`invalid SARIF column kind ${String(columnKind)}`);
		}
		const lines = source?.split(/\r\n|\n/u);
		for (const resultValue of run.results === undefined ? [] : array(run.results, 'SARIF results')) {
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
			const startLine = positiveInteger(region.startLine, 'SARIF start line');
			const startColumn = region.startColumn === undefined
				? 1
				: positiveInteger(region.startColumn, 'SARIF start column');
			const start = { row: startLine, column: characterColumn(columnKind, lines, startLine, startColumn) };
			const descriptor = resultRule(run, result);
			const reference = result.rule === undefined ? undefined : record(result.rule, 'SARIF rule reference');
			const message = record(result.message, 'SARIF message');
			const diagnostic: Diagnostic = {
				filename,
				code: text(result.ruleId ?? reference?.id ?? descriptor?.rule.id, 'SARIF rule id'),
				message: text(message.text, 'SARIF message text'),
				severity: severityOf(
					result.level === undefined ? configuredLevel(run, result, descriptor) ?? 'warning' : result.level,
				),
				location: start,
			};
			const endLine = region.endLine === undefined ? start.row : positiveInteger(region.endLine, 'SARIF end line');
			const lastLine = lines?.[endLine - 1];
			const endColumn = region.endColumn === undefined
				? lastLine === undefined ? undefined : [...lastLine].length + 1
				: characterColumn(columnKind, lines, endLine, positiveInteger(region.endColumn, 'SARIF end column'));
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

interface ResolvedRule {
	component: number;
	index: number;
	rule: Record<string, unknown>;
}

function resultRule(run: Record<string, unknown>, result: Record<string, unknown>): ResolvedRule | undefined {
	const reference = result.rule === undefined ? undefined : record(result.rule, 'SARIF rule reference');
	const componentRef = reference?.toolComponent === undefined
		? undefined
		: record(reference.toolComponent, 'SARIF tool component reference');
	const tool = run.tool === undefined ? undefined : record(run.tool, 'SARIF tool');
	const driver = tool?.driver === undefined ? undefined : record(tool.driver, 'SARIF driver');
	const extensions = tool?.extensions === undefined
		? []
		: array(tool.extensions, 'SARIF extensions').map(value => record(value, 'SARIF extension'));
	let component = -1;
	if (componentRef?.index !== undefined && componentRef.index !== -1) {
		if (
			typeof componentRef.index !== 'number' || !Number.isInteger(componentRef.index) || componentRef.index < 0
			|| componentRef.index >= extensions.length
		) throw new Error('SARIF component index is out of range');
		component = componentRef.index;
	} else if (componentRef?.guid !== undefined) {
		if (typeof componentRef.guid !== 'string') throw new Error('invalid SARIF component guid');
		if (driver?.guid !== componentRef.guid) {
			component = extensions.findIndex(value => value.guid === componentRef.guid);
			if (component < 0) throw new Error('SARIF component guid was not found');
		}
	}
	const selected = component < 0 ? driver : extensions[component];
	if (componentRef?.guid !== undefined && selected?.guid !== componentRef.guid) {
		throw new Error('conflicting SARIF component references');
	}
	const rules = selected?.rules === undefined
		? []
		: array(selected.rules, 'SARIF rules').map(value => record(value, 'SARIF rule'));
	if (result.ruleIndex !== undefined && reference?.index !== undefined && result.ruleIndex !== reference.index) {
		throw new Error('conflicting SARIF rule indices');
	}
	if (result.ruleId !== undefined && reference?.id !== undefined && result.ruleId !== reference.id) {
		throw new Error('conflicting SARIF rule ids');
	}
	const index = reference?.index ?? result.ruleIndex;
	if (index !== undefined && (typeof index !== 'number' || !Number.isInteger(index) || index < -1)) {
		throw new Error('invalid SARIF rule index');
	}
	let ruleIndex = typeof index === 'number' ? index : -1;
	if (ruleIndex < 0 && reference?.guid !== undefined) ruleIndex = rules.findIndex(rule => rule.guid === reference.guid);
	if (ruleIndex < 0 && reference?.guid === undefined) {
		ruleIndex = rules.findIndex(rule => rule.id === (reference?.id ?? result.ruleId) && rule.id !== undefined);
	}
	if (ruleIndex < 0) {
		if (reference?.guid !== undefined) throw new Error('SARIF rule guid was not found');
		return undefined;
	}
	const rule = rules[ruleIndex];
	if (rule === undefined) throw new Error('SARIF rule index is out of range');
	if (reference?.guid !== undefined && rule.guid !== reference.guid) {
		throw new Error('conflicting SARIF rule references');
	}
	return { component, index: ruleIndex, rule };
}

function configuredLevel(
	run: Record<string, unknown>,
	result: Record<string, unknown>,
	descriptor: ResolvedRule | undefined,
): unknown {
	const provenance = result.provenance === undefined ? undefined : record(result.provenance, 'SARIF provenance');
	const index = provenance?.invocationIndex;
	if (index !== undefined && index !== -1) {
		if (typeof index !== 'number' || !Number.isInteger(index) || index < 0) {
			throw new Error('invalid SARIF invocation index');
		}
		const invocations = array(run.invocations, 'SARIF invocations');
		const invocation = record(invocations[index], 'SARIF invocation');
		const overrides = invocation.ruleConfigurationOverrides === undefined
			? []
			: array(invocation.ruleConfigurationOverrides, 'SARIF rule overrides');
		for (const value of overrides) {
			const override = record(value, 'SARIF rule override');
			const reference = record(override.descriptor, 'SARIF override descriptor');
			const matched = resultRule(run, { rule: reference });
			if (
				matched !== undefined && descriptor !== undefined && matched.component === descriptor.component
				&& matched.index === descriptor.index
			) {
				const configuration = record(override.configuration, 'SARIF override configuration');
				if (configuration.level !== undefined) return configuration.level;
				break;
			}
		}
	}
	if (descriptor?.rule.defaultConfiguration === undefined) return undefined;
	return record(descriptor.rule.defaultConfiguration, 'SARIF default configuration').level;
}

function characterColumn(kind: unknown, lines: string[] | undefined, row: number, column: number): number {
	if (kind === 'unicodeCodePoints' || column === 1) return column;
	const line = lines?.[row - 1];
	if (line === undefined) throw new Error('SARIF UTF-16 column requires the captured source line');
	const offset = column - 1;
	if (kind === 'unspecified') {
		if ([...line].slice(0, offset).some(character => character.length > 1)) {
			throw new Error('SARIF column kind is required for a line containing astral characters');
		}
		if (offset > line.length) throw new Error('SARIF column exceeds the captured source line');
		return column;
	}
	if (offset > line.length) throw new Error('SARIF UTF-16 column exceeds the captured source line');
	const prefix = line.slice(0, offset);
	if (
		offset < line.length && /[\uD800-\uDBFF]/u.test(line[offset - 1] ?? '')
		&& /[\uDC00-\uDFFF]/u.test(line[offset] ?? '')
	) {
		throw new Error('SARIF UTF-16 column splits a surrogate pair');
	}
	return [...prefix].length + 1;
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
			const candidate = uri.startsWith('file:') ? fileURLToPath(uri) : decodeURIComponent(uri);
			return resolve(directory, candidate) === expected;
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
