import { randomUUID } from 'node:crypto';
import { readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { DocumentDiagnostic } from '@kjanat-actionlint/plugin-sdk';
import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { configObject, stringOption } from '@kjanat-actionlint/plugin-sdk/config';
import { withCapturedFile } from '@kjanat-actionlint/plugin-sdk/files';
import { runCommand } from '@kjanat-actionlint/plugin-sdk/process';
import { exactStagedUri, parseSarif } from '@kjanat-actionlint/plugin-sdk/sarif';

export function parseRunnerGuardSarif(
	output: string,
	filename: string,
	directory: string,
	relativeFile: string,
	source?: string,
): DocumentDiagnostic[] {
	return parseSarif(output, filename, exactStagedUri(directory, relativeFile), source);
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/runner-guard',
	version: '0.1.0-plugins.1',
	name: 'Runner Guard document provider',
	description: 'Runs Runner Guard against an isolated copy of the captured workflow.',
	license: 'MIT',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow'],
	requiredInputs: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	rule: {
		name: 'runner-guard',
		category: 'security',
		subcategory: 'workflow',
		maturity: 'stable',
		description: 'Report GitHub Actions supply-chain findings from Runner Guard.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/runner-guard',
	},
	check: async (request) => {
		const config = configObject(request.config);
		const failOn = stringOption(config, 'fail_on', ['critical', 'high', 'medium', 'low']);
		return withCapturedFile(request, async (directory, relativeFile) => {
			const report = join(tmpdir(), `runner-guard-${randomUUID()}.sarif`);
			const args = ['scan', directory, '--format', 'sarif', '--output', report];
			if (failOn !== undefined) args.push('--fail-on', failOn);
			try {
				const result = await runCommand('runner-guard', args, { allowedExitCodes: [0, 1] });
				const output = await readFile(report, 'utf8');
				if (output.trim().length === 0) {
					throw new Error(`Runner Guard produced no SARIF output${result.stderr ? `: ${result.stderr.trim()}` : ''}`);
				}
				return parseRunnerGuardSarif(output, request.filename, directory, relativeFile, request.source);
			} finally {
				await rm(report, { force: true });
			}
		});
	},
});
