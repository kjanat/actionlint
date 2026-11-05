import { defineProcessProvider, jqOutput } from '@kjanat-actionlint/plugin-sdk';

export default defineProcessProvider({
	id: 'github.com/kjanat/actionlint/examples/psscriptanalyzer',
	version: '0.1.0',
	name: 'PSScriptAnalyzer',
	description: 'PowerShell diagnostics and fixes through a typed process manifest and jq.',
	license: 'MIT',
	repository: 'https://github.com/kjanat/actionlint',
	shells: ['pwsh', 'powershell'],
	filename: 'actionlint.ps1',
	fixes: true,
	configSchema: {
		'type': 'object',
		'properties': {
			'IncludeRules': {
				'type': 'array',
				'items': {
					'type': 'string',
					'minLength': 1,
				},
				'uniqueItems': true,
			},
			'ExcludeRules': {
				'type': 'array',
				'items': {
					'type': 'string',
					'minLength': 1,
				},
				'uniqueItems': true,
			},
			'Severity': {
				'type': 'array',
				'items': {
					'enum': [
						'Information',
						'Warning',
						'Error',
						'ParseError',
					],
				},
				'uniqueItems': true,
			},
			'IncludeDefaultRules': {
				'type': 'boolean',
			},
			'Rules': {
				'type': 'object',
				'additionalProperties': {
					'type': 'object',
				},
			},
		},
		'additionalProperties': false,
	},
	entry: 'pwsh',
	args: [
		'-NoLogo',
		'-NoProfile',
		'-NonInteractive',
		'-Command',
		"$ErrorActionPreference = 'Stop'; [Console]::InputEncoding = [Text.UTF8Encoding]::new($false); [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); Import-Module PSScriptAnalyzer -MinimumVersion 1.25.0; $request = ConvertFrom-Json -AsHashtable -InputObject ([Console]::In.ReadToEnd()); ConvertTo-Json -Depth 6 -InputObject @(Invoke-ScriptAnalyzer -ScriptDefinition $request.source -Settings $request.config | Select-Object RuleName, Message, Severity, Extent, SuggestedCorrections)",
	],
	stdin: 'request',
	env_passthrough: [
		'PATH',
		'PSModulePath',
		'SystemRoot',
		'TEMP',
		'TMP',
		'HOME',
		'USERPROFILE',
		'APPDATA',
		'LOCALAPPDATA',
	],
	versionProbe: {
		'args': [
			'-NoLogo',
			'-NoProfile',
			'-NonInteractive',
			'-Command',
			"$ErrorActionPreference = 'Stop'; Import-Module PSScriptAnalyzer -MinimumVersion 1.25.0; (Get-Module PSScriptAnalyzer).Version.ToString()",
		],
		'pattern': '^([0-9]+\\.[0-9]+\\.[0-9]+)\\s*$',
		'minimum': '1.25.0',
	},
	config: { render: 'json-stdin', transform: { language: 'jq', inline: '{config:(. // {}),vars:{}}' } },
	output: {
		format: 'json',
		coordinates: { rows: 'source', columns: 'utf16', end: 'exclusive' },
		transform: jqOutput('./output.jq'),
	},
	rule: {
		name: 'psscriptanalyzer',
		description: 'Analyze PowerShell snippets with PSScriptAnalyzer',
		category: 'correctness',
		subcategory: 'powershell',
		maturity: 'stable',
		provider: 'github.com/kjanat/actionlint/examples/psscriptanalyzer',
	},
});
