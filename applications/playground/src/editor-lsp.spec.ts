import { strict as assert } from 'node:assert';
import { describe, it } from 'vitest';

import { Text } from '@codemirror/state';

import { editorRange, workspaceChanges } from './editor-lsp';

describe('LSP source coordinates', () => {
	it('uses UTF-16 positions across astral characters and lines', () => {
		const doc = Text.of(['# 🚀🚀', 'on: push']);
		assert.deepEqual(editorRange(doc, { start: { line: 0, character: 2 }, end: { line: 0, character: 6 } }), {
			from: 2,
			to: 6,
		});
		assert.deepEqual(editorRange(doc, { start: { line: 1, character: 0 }, end: { line: 1, character: 2 } }), {
			from: 7,
			to: 9,
		});
	});
	it('rejects invalid or reversed ranges instead of moving them to another location', () => {
		const doc = Text.of(['abc']);
		for (
			const range of [
				{ start: { line: -1, character: 0 }, end: { line: 0, character: 1 } },
				{ start: { line: 0, character: 1 }, end: { line: 1, character: 1 } },
				{ start: { line: 0, character: 1 }, end: { line: 0, character: 9 } },
				{ start: { line: 0, character: 2 }, end: { line: 0, character: 1 } },
			]
		) assert.throws(() => editorRange(doc, range));
	});
	it('accepts matching versioned fixes and rejects stale, foreign and overlapping edits', () => {
		const doc = Text.of(['on: foo']);
		const uri = 'file:///workspace/test.yml';
		const edits = [{ range: { start: { line: 0, character: 4 }, end: { line: 0, character: 7 } }, newText: 'push' }];
		assert.deepEqual(
			workspaceChanges(doc, { documentChanges: [{ textDocument: { uri, version: 3 }, edits }] }, uri, 3),
			[{ from: 4, to: 7, insert: 'push' }],
		);
		for (
			const edit of [
				{ documentChanges: [{ textDocument: { uri, version: 2 }, edits }] },
				{ documentChanges: [{ textDocument: { uri: 'file:///another.yml', version: 3 }, edits }] },
				{ documentChanges: [{ textDocument: { uri, version: 3 }, edits: [...edits, ...edits] }] },
				{ changes: { [uri]: edits } },
			]
		) assert.equal(workspaceChanges(doc, edit, uri, 3), null);
	});
});
