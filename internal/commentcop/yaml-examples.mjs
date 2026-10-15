// @ts-check

/** @param {string | null | undefined} text */
function yamlMapping(text) {
	if (text === null || text === undefined) return null;
	const match = /^(\s*)(-\s+)?([\w./*{}$-]+|"(?:[^"\\]|\\.)*"|'(?:[^']|'')*')\s*:(?:\s+(.*))?$/.exec(text);
	if (match === null) return null;
	return {
		indent: match[1].length + (match[2]?.length ?? 0),
		sequence: match[2] !== undefined,
		value: match[4]?.trim() ?? '',
		proseLabel: /^(?:note|warning|important|tip|caution|todo|fixme)$/i.test(match[3]),
	};
}

/** @param {string[]} lines */
export function yamlExampleLines(lines) {
	const examples = new Set();
	const textLines = lines.map(line =>
		line.trimStart().startsWith('#') && !line.trimStart().startsWith('#!')
			? line.trimStart().replace(/^#\s?/, '')
			: null
	);
	const mappings = textLines.map(yamlMapping);
	/** @param {number} index */
	const scalarEntry = index => {
		const mapping = mappings[index];
		return mapping && !mapping.proseLabel && mapping.value !== '' ? mapping.indent : null;
	};
	/** @type {number | null} */
	let parentIndent = null;
	for (const [index, text] of textLines.entries()) {
		if (text === null || text === undefined) {
			parentIndent = null;
			continue;
		}
		if (text.trimStart().startsWith('#')) continue;
		const mapping = mappings[index];
		const indent = mapping?.indent ?? text.length - text.trimStart().length;
		const nested = parentIndent !== null && indent > parentIndent;

		if (mapping !== null) {
			const value = mapping.value;
			const child = textLines.slice(index + 1).find(line =>
				line === null || line !== undefined && !line.trimStart().startsWith('#')
			);
			const container = /^[[{]/.test(value) || value === '' && child !== null && child !== undefined
					&& child.length - child.trimStart().length > indent
					&& (yamlMapping(child) !== null || /^\s*-\s/.test(child));
			const scalar = /^(?:true|false|null|[-+]?\d+(?:\.\d+)?)(?:\s+#.*)?$/.test(value);
			const adjacentScalars = scalarEntry(index) === indent
				&& (scalarEntry(index - 1) === indent || scalarEntry(index + 1) === indent);
			if (nested || container || scalar || adjacentScalars || mapping.sequence && !mapping.proseLabel) {
				examples.add(index + 1);
			}
			if (container && !nested) parentIndent = indent;
			else if (!nested) parentIndent = null;
		} else if (nested && (/^\s*-\s+\S/.test(text) || /^\s*[[\]{}][\s,]*$/.test(text))) {
			examples.add(index + 1);
		} else if (text.trim() !== '') {
			parentIndent = null;
		}
	}
	return examples;
}
