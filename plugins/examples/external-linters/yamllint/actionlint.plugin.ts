import type { DocumentDiagnostic } from '@kjanat-actionlint/plugin-sdk';
import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { booleanOption, configObject } from '@kjanat-actionlint/plugin-sdk/config';
import { runCommand } from '@kjanat-actionlint/plugin-sdk/process';

export function parseYamllint(output: string, filename: string): DocumentDiagnostic[] {
	const diagnostics: DocumentDiagnostic[] = [];
	for (const line of output.split(/\r?\n/u)) {
		if (line.length === 0) continue;
		const match = /^stdin:(\d+):(\d+): \[(error|warning)\] (.+?)(?: \(([^()]*)\))?$/u.exec(line);
		if (match === null) throw new Error(`yamllint returned unrecognized output: ${line}`);
		const row = Number(match[1]);
		const column = Number(match[2]);
		const severity = match[3];
		const message = match[4];
		const rule = match[5];
		if (!Number.isInteger(row) || row < 1 || !Number.isInteger(column) || column < 1) {
			throw new Error(`yamllint returned an invalid position: ${line}`);
		}
		if (severity !== 'error' && severity !== 'warning') {
			throw new Error(`yamllint returned an invalid severity: ${line}`);
		}
		if (message === undefined) throw new Error(`yamllint returned an empty message: ${line}`);
		diagnostics.push({
			filename,
			code: rule ?? 'yamllint',
			message,
			severity,
			location: { row, column },
		});
	}
	return diagnostics;
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/yamllint',
	version: '0.1.0-plugins.1',
	name: 'yamllint document provider',
	description: 'Runs yamllint against the captured workflow or action YAML.',
	license: 'MIT',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow', 'action'],
	requiredInputs: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	rule: {
		name: 'yamllint',
		category: 'conventions',
		subcategory: 'yaml',
		maturity: 'stable',
		description: 'Report YAML syntax and style findings from yamllint.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/yamllint',
	},
	check: async (request) => {
		const config = configObject(request.config);
		const strict = booleanOption(config, 'strict') === true;
		const args = ['-f', 'parsable'];
		if (strict) args.push('--strict');
		const configData = config.config_data;
		if (configData !== undefined) {
			if (typeof configData !== 'string' || configData.length === 0 || configData.length > 65536) {
				throw new Error('plugin config config_data must be 1 to 65536 YAML characters');
			}
			args.push('--config-data', configData);
		}
		args.push('-');
		const result = await runCommand('yamllint', args, {
			stdin: request.source,
			allowedExitCodes: strict ? [0, 1, 2] : [0, 1],
		});
		if (result.stdout.length === 0 && result.exitCode !== 0) {
			const details = result.stderr.trim();
			throw new Error(
				`yamllint exited ${result.exitCode} without findings${details.length > 0 ? `: ${details}` : ''}`,
			);
		}
		const findings = parseYamllint(result.stdout, request.filename);
		if (
			result.exitCode === 2
			&& (!strict || result.stderr.trim().length > 0 || findings.length === 0
				|| findings.some((finding) => finding.severity !== 'warning'))
		) {
			throw new Error('yamllint strict mode exited 2 without only warning findings');
		}
		return findings;
	},
});
