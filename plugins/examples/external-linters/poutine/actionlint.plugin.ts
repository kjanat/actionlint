import type { DocumentDiagnostic } from '@kjanat-actionlint/plugin-sdk';
import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { configObject } from '@kjanat-actionlint/plugin-sdk/config';
import { withCapturedFile } from '@kjanat-actionlint/plugin-sdk/files';
import { runCommand, text } from '@kjanat-actionlint/plugin-sdk/process';
import { exactStagedUri, parseSarif } from '@kjanat-actionlint/plugin-sdk/sarif';

export function parsePoutineSarif(
	output: string,
	filename: string,
	directory: string,
	relativeFile: string,
	source?: string,
): DocumentDiagnostic[] {
	return parseSarif(output, filename, exactStagedUri(directory, relativeFile), source);
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/poutine',
	version: '0.1.0-plugins.1',
	name: 'Poutine document provider',
	description: 'Runs Poutine against an isolated copy of the captured workflow.',
	license: 'Apache-2.0',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow'],
	requiredInputs: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	rule: {
		name: 'poutine',
		category: 'security',
		subcategory: 'workflow',
		maturity: 'stable',
		description: 'Report pipeline supply-chain findings from Poutine.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/poutine',
	},
	check: async (request) => {
		const config = configObject(request.config);
		const configuredFile = config.config_file;
		if (configuredFile !== undefined && typeof configuredFile !== 'string') {
			throw new Error('plugin config config_file must be a path string');
		}
		return withCapturedFile(request, async (directory, relativeFile) => {
			const args = ['analyze_local', directory, '--format', 'sarif', '--disable-version-check'];
			if (typeof configuredFile === 'string') {
				args.push('--config', text(configuredFile, 'plugin config config_file'));
			}
			const result = await runCommand('poutine', args);
			if (result.stdout.trim().length === 0) {
				throw new Error(`Poutine produced no SARIF output${result.stderr ? `: ${result.stderr.trim()}` : ''}`);
			}
			return parsePoutineSarif(result.stdout, request.filename, directory, relativeFile, request.source);
		});
	},
});
