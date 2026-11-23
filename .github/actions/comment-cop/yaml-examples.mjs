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
	/** @type {number | null} */
	let blockIndent = null;
	for (const [index, text] of textLines.entries()) {
		if (text === null || text === undefined) {
			parentIndent = null;
			blockIndent = null;
			continue;
		}
		if (blockIndent !== null) {
			if (text.trim() === '' || text.length - text.trimStart().length > blockIndent) {
				examples.add(index + 1);
				continue;
			}
			blockIndent = null;
		}
		if (text.trimStart().startsWith('#')) continue;
		const mapping = mappings[index];
		const indent = mapping?.indent ?? text.length - text.trimStart().length;
		const nested = parentIndent !== null && indent > parentIndent;
		const blockValue = mapping?.value ?? /^\s*-\s+(.+)$/.exec(text)?.[1].trim();
		if (blockValue && /^[|>](?:[+-][1-9]?|[1-9][+-]?)?(?:\s+#.*)?$/.test(blockValue)) {
			blockIndent = indent;
			examples.add(index + 1);
			continue;
		}

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

if (process.env.NODE_TEST_CONTEXT) {
	const { test } = await import('node:test');
	/** @type {typeof import('node:assert').strict} */
	const assert = (await import('node:assert')).strict;

	/** @param {string[]} lines @param {number[]} expected */
	const check = (lines, expected) => assert.deepEqual([...yamlExampleLines(lines)], expected, lines.join('\n'));

	test('separates prose from commented YAML examples', () => {
		const lines = [
			'# Tools are discovered on PATH.',
			'# Python scripts are linted without execution.',
			'# tools:',
			'#   ruff: {select: [F, B, S]}',
			'#   shellcheck:',
			'#     # Inline directives remain active.',
			'#     config: {shell: bash}',
			'',
			'# local-actions:',
			'#   ./generated-action: ./src/action',
			'# files:',
			'#   includes:',
			'#     - "**/*.yaml"',
			'#     - "rather than.yml"',
			'# ignores:',
			'#   - rule: shellcheck',
			'#     reason: Use the robust compatibility wrapper rather than the default.',
			'',
			'# Use defaults rather than loading another file.',
		];
		check(lines, [3, 4, 5, 7, 9, 10, 11, 12, 13, 14, 15, 16, 17]);
	});

	test('treats YAML sequence items as examples unless they are prose', () => {
		check(['# - Use the cache rather than parsing twice.'], []);
		check(['# - Note: use this rather than that'], []);
		check(['# - name: build rather than deploy'], [1]);
		for (const [prefix, key] of [['- ', 'name'], ['  - ', '"name"'], ['-   ', "'name'"]]) {
			const indent = ' '.repeat(prefix.length);
			const lines = [
				`# ${prefix}${key}: build rather than deploy`,
				`# ${indent}run: echo it`,
				`# ${indent}shell: bash`,
			];
			check(lines, [1, 2, 3]);
		}
	});

	test('keeps labeled prose out of adjacent examples', () => {
		check(
			['# Note: use the wrapper.', '# tools:', '#   ruff:', '#     select: [F]', '# Warning: be careful.', '# note: x'],
			[2, 3, 4],
		);
		check(
			[
				'# Note: use the wrapper.',
				'# image: ubuntu',
				'# shell: bash',
				'# working-directory: src',
				'# Warning: be careful.',
			],
			[2, 3, 4],
		);
	});

	test('treats commented block scalars as examples until their indentation ends', () => {
		const headers = [['run:', '  '], ['- run:', '    '], ['-', '  '], ['  run:', '    ']];
		for (
			const [index, indicator] of ['|', '|-', '|+', '>', '>-', '>+', '|2', '|2-', '|-2', '>2+', '>+2', '| # x']
				.entries()
		) {
			const [header, indent] = headers[index % headers.length];
			const lines = [`# ${header} ${indicator}`, `# ${indent}echo it`, '#', `# ${indent}Note: text`, '# Note: text'];
			check(lines, [1, 2, 3, 4]);
		}
	});
}
