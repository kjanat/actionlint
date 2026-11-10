import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

import { expect, test } from 'bun:test';

import { exactStagedUri, parseSarif } from '#sarif';

test('SARIF accepts only the staged document after decoding its URI once', () => {
	for (const name of ['a%20b', 'a%b', 'a%2Fb']) {
		const directory = resolve(tmpdir(), name);
		const allowed = exactStagedUri(directory, 'captured.yml');
		expect(allowed(pathToFileURL(resolve(directory, 'captured.yml')).href)).toBe(true);
		expect(allowed(pathToFileURL(resolve(directory, 'other.yml')).href)).toBe(false);
		expect(allowed(pathToFileURL(resolve(directory, '..', 'captured.yml')).href)).toBe(false);
	}
	const allowed = exactStagedUri(tmpdir(), 'a%20b/captured file.yml');
	expect(allowed('a%2520b/captured%20file.yml')).toBe(true);
	expect(allowed('a%20b/captured%20file.yml')).toBe(false);
	expect(allowed('../captured%20file.yml')).toBe(false);
	expect(allowed('a%2/captured%20file.yml')).toBe(false);
});

function log(suppressions: unknown, kind?: string, region: Record<string, number> = { startLine: 1 }): string {
	return JSON.stringify({
		version: '2.1.0',
		runs: [{
			columnKind: 'unicodeCodePoints',
			results: [{
				ruleId: 'X1',
				message: { text: 'finding' },
				level: 'warning',
				kind,
				suppressions,
				locations: [{
					physicalLocation: { artifactLocation: { uri: 'workflow.yml' }, region },
				}],
			}],
		}],
	});
}

test('SARIF preserves active findings and respects native suppression decisions', () => {
	for (
		const [suppressions, count] of [
			[undefined, 1],
			[null, 1],
			[[], 1],
			[[{ kind: 'inSource' }], 0],
			[[{ kind: 'external', status: 'accepted' }], 0],
			[[{ kind: 'inSource', status: 'underReview' }], 1],
			[[{ kind: 'inSource', status: 'rejected' }], 1],
			[[{ kind: 'inSource', status: null }], 0],
			[[{ kind: 'external', status: 'rejected' }, { kind: 'inSource', status: 'accepted' }], 0],
		] satisfies [unknown, number][]
	) {
		expect(parseSarif(log(suppressions), 'workflow.yml', uri => uri === 'workflow.yml')).toHaveLength(count);
	}
	expect(parseSarif(log(undefined, 'pass'), 'workflow.yml', () => true)).toHaveLength(0);
	expect(parseSarif(log(undefined, ''), 'workflow.yml', () => true)).toHaveLength(1);
});

test('SARIF defaults optional end coordinates and reads line ends from captured source', () => {
	const read = (region: Record<string, number>, source?: string) =>
		parseSarif(log(undefined, undefined, region), 'workflow.yml', () => true, source)[0];
	expect(read({ startLine: 1, endLine: 1 })?.end_location).toBeUndefined();
	expect(read({ startLine: 1, endLine: 1 }, 'echo hello\r\n')?.end_location).toEqual({ row: 1, column: 11 });
	expect(read({ startLine: 1, endColumn: 5 })?.end_location).toEqual({ row: 1, column: 5 });
	expect(read({ startLine: 1, endLine: 2 }, 'first\necho 😀\n')?.end_location).toEqual({ row: 2, column: 7 });
	expect(() => read({ startLine: 2, endLine: 1 })).toThrow('ends before its start');
});

test('SARIF rejects malformed native suppression records instead of hiding findings', () => {
	for (
		const suppressions of [
			{},
			[{}],
			[{ kind: 'mystery' }],
			[{ kind: 'inSource', status: 'mystery' }],
			[{ kind: 'inSource', status: '' }],
			[{ kind: 'inSource', status: false }],
			[{ kind: 'inSource', status: 'accepted' }, { kind: 'invalid' }],
		]
	) {
		expect(() => parseSarif(log(suppressions), 'workflow.yml', () => true)).toThrow(/suppression/);
	}
});
