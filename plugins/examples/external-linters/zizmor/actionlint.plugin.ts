import type { DocumentDiagnostic } from '@kjanat-actionlint/plugin-sdk';
import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { configObject, stringOption } from '@kjanat-actionlint/plugin-sdk/config';
import { runCommand } from '@kjanat-actionlint/plugin-sdk/process';
import { parseSarif } from '@kjanat-actionlint/plugin-sdk/sarif';

export function parseZizmorSarif(output: string, filename: string): DocumentDiagnostic[] {
	return parseSarif(output, filename, (uri) => uri === '<stdin>' || uri === '-');
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/zizmor',
	version: '0.1.0-plugins.1',
	name: 'zizmor document provider',
	description: 'Runs offline zizmor audits against the captured workflow or action YAML.',
	license: 'MIT',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow', 'action'],
	requiredInputs: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	rule: {
		name: 'zizmor',
		category: 'security',
		subcategory: 'workflow',
		maturity: 'stable',
		description: 'Report workflow and action security findings from zizmor.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/zizmor',
	},
	check: async (request) => {
		const config = configObject(request.config);
		const persona = stringOption(config, 'persona', ['auditor', 'pedantic']);
		const args = /* dprint-ignore */ [
			'--format', 'sarif',
			'--offline',
			'--no-online-audits',
			'--no-config',
			'--no-exit-codes',
		];
		if (persona !== undefined) args.push('--persona', persona);
		args.push('-');
		const result = await runCommand('zizmor', args, { stdin: request.source });
		if (result.stdout.trim().length === 0) {
			throw new Error(`zizmor produced no SARIF output${result.stderr ? `: ${result.stderr.trim()}` : ''}`);
		}
		return parseZizmorSarif(result.stdout, request.filename);
	},
});
