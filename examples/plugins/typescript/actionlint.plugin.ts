import { type Diagnostic, defineScriptProvider } from '@kjanat-actionlint/plugin-sdk';

function trailingWhitespace(source: string, filename: string): Diagnostic[] {
	const diagnostics: Diagnostic[] = [];
	const lines = source.split('\n');
	lines.forEach((line, index) => {
		const content = line.endsWith('\r') ? line.slice(0, -1) : line;
		let end = content.length;
		while (end > 0 && (content[end - 1] === ' ' || content[end - 1] === '\t')) end -= 1;
		if (end === content.length) return;
		const column = [...content.slice(0, end)].length + 1;
		const endColumn = [...content].length + 1;
		diagnostics.push({
			filename,
			code: 'TRAILING_SPACE',
			message: 'Line has trailing whitespace',
			severity: 'warning',
			location: { row: index + 1, column },
			end_location: { row: index + 1, column: endColumn },
			fixes: [{
				description: 'Remove trailing whitespace',
				replacements: [{
					row: index + 1,
					column,
					end_row: index + 1,
					end_column: endColumn,
					text: '',
				}],
			}],
		});
	});
	return diagnostics;
}

export default defineScriptProvider({
	id: 'github.com/kjanat/actionlint/examples/trailing-space',
	version: '0.1.0',
	name: 'Trailing whitespace example',
	description: 'Reports and removes trailing spaces and tabs in a script source.',
	license: 'MIT',
	repository: 'https://github.com/kjanat/actionlint',
	shells: ['bash'],
	rule: {
		name: 'trailing-space',
		category: 'conventions',
		subcategory: 'formatting',
		maturity: 'nursery',
		description: 'Lines should not end in spaces or tabs.',
		provider: 'github.com/kjanat/actionlint/examples/trailing-space',
	},
	fixes: true,
	check: ({ source, filename }) => trailingWhitespace(source, filename),
});
