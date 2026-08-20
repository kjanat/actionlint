import type { Text } from '@codemirror/state';

// CodeMirror positions count UTF-16 code units. actionlint counts code points.
function utf16Offset(text: string, column: number): number {
	let offset = 0;
	for (let n = 1; n < column; n++) {
		const point = text.codePointAt(offset);
		if (point === undefined) {
			return text.length;
		}
		offset += point > 0xffff ? 2 : 1;
	}
	return offset;
}

export function errorRange(doc: Text, error: ActionlintError): { from: number; to: number } {
	const line = doc.line(Math.min(Math.max(error.line, 1), doc.lines));
	const from = Math.min(line.from + utf16Offset(line.text, error.column), line.to);
	const to = Math.min(line.from + utf16Offset(line.text, error.endColumn + 1), line.to);
	return { from, to: Math.max(from, to) };
}
