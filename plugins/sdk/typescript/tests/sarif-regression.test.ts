import { readFile } from 'node:fs/promises';

import { expect, test } from 'bun:test';

import { parseSarif } from '#sarif';

const finding = {
	ruleId: 'R',
	message: { text: 'finding' },
	locations: [{ physicalLocation: { artifactLocation: { uri: 'workflow.yml' }, region: { startLine: 1 } } }],
};
const decode = (runs: unknown[], source?: string) =>
	parseSarif(JSON.stringify({ version: '2.1.0', runs }), 'workflow.yml', uri => uri === 'workflow.yml', source);

test('metadata-only runs do not hide findings and malformed results fail', () => {
	expect(decode([{}, { results: [finding] }])).toHaveLength(1);
	expect(() => decode([{ results: null }])).toThrow('SARIF results');
});

test('rule references and configured severity resolve in precedence order', () => {
	const rule = { id: 'R', defaultConfiguration: { level: 'note' } };
	const tool = { driver: { rules: [rule] } };
	const invocations = [{
		ruleConfigurationOverrides: [{ descriptor: { index: 0 }, configuration: { level: 'error' } }],
	}];
	const base = { ...finding, ruleId: undefined, ruleIndex: 0 };
	expect(decode([{ tool, results: [base] }])[0]).toMatchObject({ code: 'R', severity: 'info' });
	expect(decode([{ tool, results: [{ ...base, ruleIndex: undefined, rule: { index: 0 } }] }])[0]?.code).toBe('R');
	expect(decode([{ tool, invocations, results: [{ ...base, provenance: { invocationIndex: 0 } }] }])[0]?.severity).toBe(
		'error',
	);
	expect(
		decode([{ tool, invocations, results: [{ ...base, level: 'warning', provenance: { invocationIndex: 0 } }] }])[0]
			?.severity,
	).toBe('warning');
	expect(decode([{ results: [finding] }])[0]?.severity).toBe('warning');
	expect(decode([{ tool, results: [{ ...finding, ruleIndex: -1 }] }])[0]?.severity).toBe('info');
	expect(() => decode([{ tool, results: [{ ...base, ruleIndex: 2 }] }])).toThrow('out of range');
});

test('UTF-16 coordinates convert using the captured lines', () => {
	const result = {
		...finding,
		locations: [{
			physicalLocation: {
				artifactLocation: { uri: 'workflow.yml' },
				region: { startLine: 1, startColumn: 4, endLine: 2, endColumn: 4 },
			},
		}],
	};
	const run = { columnKind: 'utf16CodeUnits', results: [result] };
	expect(decode([run], '😀ab\n😀xy')[0]).toMatchObject({
		location: { row: 1, column: 3 },
		end_location: { row: 2, column: 3 },
	});
	expect(() => decode([run])).toThrow('captured source line');
	expect(() => decode([run], '😀ab')).toThrow('captured source line');
	expect(() => decode([run], 'x\nx')).toThrow('exceeds');
	const split = {
		...result,
		locations: [{
			physicalLocation: { artifactLocation: { uri: 'workflow.yml' }, region: { startLine: 1, startColumn: 2 } },
		}],
	};
	expect(() => decode([{ ...run, results: [split] }], '😀')).toThrow('surrogate pair');
});

test('captured Poutine SARIF reports its line-only range', async () => {
	const base = new URL('../../../examples/external-linters/testdata/', import.meta.url);
	const output = await readFile(new URL('poutine-1.1.6.sarif.json', base), 'utf8');
	const source = await readFile(new URL('poutine-workflow.yml', base), 'utf8');
	const findings = parseSarif(output, 'workflow.yml', uri => uri.endsWith('.github/workflows/workflow.yml'), source);
	expect(findings[0]).toMatchObject({
		code: 'injection',
		location: { row: 11, column: 1 },
		end_location: { row: 11, column: 59 },
	});
});

test('tool component references resolve extension rules and overrides', () => {
	const rule = { id: 'shared', guid: 'rule-guid', defaultConfiguration: { level: 'note' } };
	const tool = {
		driver: { guid: 'driver-guid', rules: [{ id: 'shared', defaultConfiguration: { level: 'warning' } }] },
		extensions: [{ guid: 'extension-guid', rules: [rule] }],
	};
	const reference = { index: 0, toolComponent: { index: 0, guid: 'extension-guid' } };
	const result = { ...finding, ruleId: undefined, rule: reference, provenance: { invocationIndex: 0 } };
	const invocations = [{
		ruleConfigurationOverrides: [{
			descriptor: { guid: 'rule-guid', toolComponent: { guid: 'extension-guid' } },
			configuration: { level: 'error' },
		}],
	}];
	expect(decode([{ tool, invocations, results: [result] }])[0]).toMatchObject({ code: 'shared', severity: 'error' });
	expect(
		decode([{
			tool,
			results: [{
				...result,
				provenance: undefined,
				rule: { guid: 'rule-guid', toolComponent: { guid: 'extension-guid' } },
			}],
		}])[0]?.severity,
	).toBe('info');
	expect(
		decode([{
			tool,
			results: [{ ...finding, ruleId: undefined, rule: { index: 0, toolComponent: { guid: 'driver-guid' } } }],
		}])[0]?.severity,
	).toBe('warning');
});

test('missing column unit cannot shift an astral character', () => {
	const result = {
		...finding,
		locations: [{
			physicalLocation: { artifactLocation: { uri: 'workflow.yml' }, region: { startLine: 1, startColumn: 4 } },
		}],
	};
	expect(() => decode([{ results: [result] }], '😀abc')).toThrow('column kind');
	expect(decode([{ results: [result] }], 'abc😀')[0]?.location.column).toBe(4);
	expect(() => decode([{ results: [result] }])).toThrow('captured source');
	expect(decode([{ columnKind: 'unicodeCodePoints', results: [result] }], '😀abc')[0]?.location.column).toBe(4);
});

test('external properties and nonstandard line breaks are rejected', () => {
	expect(() => decode([{ externalPropertyFileReferences: {}, results: [] }])).toThrow('external property');
	expect(() => decode([{ newlineSequences: ['\r'], results: [] }])).toThrow('newline');
});
