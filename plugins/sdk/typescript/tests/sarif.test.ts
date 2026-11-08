import { expect, test } from 'bun:test';

import { parseSarif } from '#sarif';

function log(suppressions: unknown, kind?: string, region: Record<string, number> = { startLine: 1 }): string {
	return JSON.stringify({
		version: '2.1.0',
		runs: [{
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
