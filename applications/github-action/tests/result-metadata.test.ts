import assert from 'node:assert/strict';
import { test } from 'node:test';

import { parseResult } from '#result';

const base = {
	schema_version: 1,
	completed: true,
	status: 'success',
	exit_code: 0,
	file_count: 0,
	diagnostics: [],
	configurations: [],
	hints: [],
};

test('persisted incomplete checks preserve source revision and reject malformed records', () => {
	const record = {
		source: { document: '/work/ci.yml', revision: 'abc', node: '', address: '' },
		rule: 'workflow-run-names',
		reason: 'producer names unavailable',
	};
	const value = { ...base, incomplete_checks: [record] };
	assert.deepEqual(parseResult(value), value);
	for (const invalid of [null, { ...record, source: null }, { ...record, reason: '' }, { ...record, rule: '' }]) {
		assert.throws(() => parseResult({ ...base, incomplete_checks: [invalid] }));
	}
});

test('persisted incomplete checks identify calls and reject incomplete invocation identities', () => {
	const entry = { document: '/work/ci.yml', revision: 'abc', node: 'root', address: '' };
	const record = {
		source: { document: '/work/action.yml', revision: 'def', node: '', address: '' },
		invocation: { entry, id: 'a'.repeat(64) },
		rule: 'shellcheck',
		reason: 'tool unavailable',
	};
	const value = { ...base, incomplete_checks: [record, { ...record, invocation: { entry, id: 'b'.repeat(64) } }] };
	assert.deepEqual(parseResult(value), value);
	for (
		const invocation of [null, {}, { entry }, { id: 'a'.repeat(64) }, { entry, id: 'invalid' }, {
			entry: { ...entry, revision: '' },
			id: 'a'.repeat(64),
		}]
	) {
		assert.throws(() => parseResult({ ...base, incomplete_checks: [{ ...record, invocation }] }));
	}
});

const selection = {
	host: 'github.com',
	repository: 'owner/repository',
	repository_id: 42,
	commit: '0123456789abcdef0123456789abcdef01234567',
	manifest: 'action.yml',
	selected_object: '0123456789abcdef0123456789abcdef01234567',
};

test('persisted action metadata accepts each availability state and zero manifest coordinates', () => {
	const records = [
		{ reference: 'owner/action@v1', state: 'unavailable', reason: 'offline' },
		{ reference: 'owner/action@v1', state: 'available', ...selection },
		{
			reference: 'owner/action@v1',
			state: 'invalid',
			...selection,
			problems: [{
				message: 'invalid metadata',
				path: 'action.yml',
				start: { line: 0, column: 0 },
				end: { line: 0, column: 0 },
			}],
		},
	];
	const value = { ...base, action_metadata: records };
	assert.deepEqual(parseResult(value), value);
});

test('persisted action metadata accepts a selection without a repository ID', () => {
	const { repository_id: _, ...unidentified } = selection;
	const value = {
		...base,
		action_metadata: [{ reference: 'owner/action@v1', state: 'available', ...unidentified }],
	};
	assert.deepEqual(parseResult(value), value);
	for (const repository_id of [0, -1, 1.5, '42', null]) {
		assert.throws(() =>
			parseResult({
				...base,
				action_metadata: [{ reference: 'owner/action@v1', state: 'available', ...unidentified, repository_id }],
			})
		);
	}
});

test('persisted action metadata rejects malformed state, reason and problem variants', () => {
	const valid = { reference: 'owner/action@v1', state: 'unavailable', reason: 'offline' };
	const invalidRecords = [
		{ ...valid, state: 'unknown' },
		{ ...valid, reason: 'invented' },
		{ reference: valid.reference, state: 'unavailable' },
		{ reference: valid.reference, state: 'available', ...selection, reason: 'offline' },
		{ reference: valid.reference, state: 'available', ...selection, problems: [] },
		{ reference: valid.reference, state: 'invalid', ...selection },
		{ reference: valid.reference, state: 'invalid', ...selection, problems: [] },
		{
			reference: valid.reference,
			state: 'invalid',
			...selection,
			reason: 'offline',
			problems: [{ message: 'bad', path: 'action.yml', start: { line: 1, column: 1 }, end: { line: 1, column: 1 } }],
		},
		{
			reference: valid.reference,
			state: 'invalid',
			...selection,
			problems: [{ message: 'bad', path: 'action.yml', start: { line: -1, column: 0 }, end: { line: 0, column: 0 } }],
		},
	];
	for (const record of invalidRecords) {
		assert.throws(() => parseResult({ ...base, action_metadata: [record] }));
	}
});

test('persisted results without action metadata remain compatible', () => {
	assert.deepEqual(parseResult(base), base);
});
