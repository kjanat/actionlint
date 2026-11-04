import type { ChangeSpec, Text } from '@codemirror/state';
import type { Range, WorkspaceEdit } from '@kjanat-actionlint/language-server';

/** LSP and CodeMirror both count UTF-16 code units. Reject invalid ranges. */
export function editorRange(doc: Text, range: Range): { from: number; to: number } {
	function offset(line: number, character: number): number {
		if (!Number.isInteger(line) || line < 0 || line >= doc.lines || !Number.isInteger(character) || character < 0) {
			throw new Error('The language server returned an invalid source range');
		}
		const row = doc.line(line + 1);
		if (character > row.length) throw new Error('The language server returned a column outside the source line');
		return row.from + character;
	}
	const from = offset(range.start.line, range.start.character);
	const to = offset(range.end.line, range.end.character);
	if (to < from) throw new Error('The language server returned a reversed source range');
	return { from, to };
}

/** Accept only edits for this exact open document version. The playground has
 * one editable buffer; VS Code clients apply multi-file edits through its API. */
export function workspaceChanges(doc: Text, edit: WorkspaceEdit, uri: string, version: number): ChangeSpec[] | null {
	if (edit.changes || !edit.documentChanges?.length) return null;
	const changes: { from: number; to: number; insert: string }[] = [];
	for (const change of edit.documentChanges) {
		if (!('textDocument' in change) || change.textDocument.uri !== uri || change.textDocument.version !== version) {
			return null;
		}
		for (const replacement of change.edits) {
			if (!('newText' in replacement)) return null;
			changes.push({ ...editorRange(doc, replacement.range), insert: replacement.newText });
		}
	}
	changes.sort((a, b) => a.from - b.from || a.to - b.to);
	for (let i = 1; i < changes.length; i++) {
		const current = changes[i];
		const previous = changes[i - 1];
		if (current && previous && current.from < previous.to) return null;
	}
	return changes;
}
