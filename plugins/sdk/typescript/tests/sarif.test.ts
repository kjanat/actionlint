import { expect, test } from 'bun:test';

import { parseSarif } from '#sarif';

function log(suppressions: unknown, kind?: string): string {
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
					physicalLocation: { artifactLocation: { uri: 'workflow.yml' }, region: { startLine: 1 } },
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
