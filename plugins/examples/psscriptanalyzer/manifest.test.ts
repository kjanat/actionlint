import { test } from 'node:test';

import { readFile } from 'node:fs/promises';

import { createPluginTester } from '@kjanat-actionlint/plugin-sdk/testing';

const plugin = createPluginTester(new URL('./actionlint.plugin.ts', import.meta.url));

test('recorded PSScriptAnalyzer output maps UTF-16 ranges and suggested corrections', async () => {
	const stdout = await readFile(new URL('./testdata/alias.json', import.meta.url), 'utf8');
	await plugin.assert([{
		name: 'alias after emoji',
		request: { source: "Write-Output '😀'; gci .", filename: 'actionlint.ps1' },
		stdout,
		expect: {
			diagnostics: [{
				filename: 'actionlint.ps1',
				code: 'PSAvoidUsingCmdletAliases',
				message:
					"'gci' is an alias of 'Get-ChildItem'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content.",
				severity: 'warning',
				location: { row: 1, column: 19 },
				end_location: { row: 1, column: 22 },
				fixes: [{
					description: 'Replace gci with Get-ChildItem',
					replacements: [{ row: 1, column: 19, end_row: 1, end_column: 22, text: 'Get-ChildItem' }],
				}],
			}],
		},
	}, {
		name: 'clean PowerShell',
		request: { source: 'Get-ChildItem .', filename: 'actionlint.ps1' },
		stdout: '[]',
		expect: { diagnostics: [] },
	}, {
		name: 'unsupported setting rejected by configuration schema',
		request: { source: 'gci .', filename: 'actionlint.ps1', config: { UnknownSetting: true } },
		expect_error: "additional properties 'UnknownSetting' not allowed",
	}]);
});
