import assert from 'node:assert/strict';
import { readFile, realpath } from 'node:fs/promises';
import { createRequire } from 'node:module';

// Use an installed language server without adding an editor dependency to builds.
const require = createRequire(await realpath(process.argv[2]));
let getLanguageService, TextDocument;
try {
	({ getLanguageService } = require('yaml-language-server/lib/umd/languageservice/yamlLanguageService.js'));
	({ TextDocument } = require('vscode-languageserver-textdocument'));
} catch (error) {
	if (error.code !== 'MODULE_NOT_FOUND') throw error;
	console.log('Installed YAML language server does not expose its language service');
	process.exit(77);
}

const schemaURI = new URL('../../actionlint.schema.json', import.meta.url).href;
const service = getLanguageService({
	workspaceContext: { resolveRelativePath: (relative, resource) => new URL(relative, resource).href },
	schemaRequestService: async (uri) => {
		assert.equal(new URL(uri).protocol, 'file:', `Unexpected network schema request: ${uri}`);
		return readFile(new URL(uri), 'utf8');
	},
});
service.configure({
	completion: true,
	validate: true,
	yamlVersion: '1.2',
	schemas: [{ uri: schemaURI, fileMatch: ['*'] }],
});
let serial = 0;
const document = (text) => TextDocument.create(`file:///review/ruff-${serial++}.yaml`, 'yaml', 1, text);
const config = (key, value, override) => {
	const entry = { tools: { ruff: { [key]: value } } };
	return override ? { overrides: [{ includes: ['**'], ...entry }] } : entry;
};

for (const override of [false, true]) {
	for (const key of ['select', 'ignore', 'target-version']) {
		const prefix = override ? 'overrides:\n  - includes: ["**"]\n    tools:\n      ruff:\n' : 'tools:\n  ruff:\n';
		const indent = override ? '        ' : '    ';
		const tail = key === 'target-version' ? `${indent}${key}: ` : `${indent}${key}:\n${indent}  - `;
		const text = prefix + tail;
		const lines = text.split('\n');
		const result = await service.doComplete(document(text + '\n'), {
			line: lines.length - 1,
			character: lines.at(-1).length,
		}, false);
		const labels = result.items.map((item) => item.label);
		if (key === 'target-version') {
			const upstream = JSON.parse(
				await readFile(new URL('../../schemas/ruff/0.17.0.schema.json', import.meta.url), 'utf8'),
			);
			assert.deepEqual(
				labels.filter((label) => label !== 'null').sort(),
				[...upstream.definitions.PythonVersion.enum].sort(),
			);
		} else {
			assert.deepEqual(labels.sort(), ['F', 'F821'], `${key}, override=${override}: unsafe or missing completions`);
		}
	}
	for (const key of ['select', 'ignore']) {
		for (const value of [null, [], ['F821'], ['C9', 'U004', 'PGH001']]) {
			const errors = await service.doValidation(document(JSON.stringify(config(key, value, override))), false);
			assert.equal(
				errors.length,
				0,
				`${key}=${JSON.stringify(value)}, override=${override}: ${JSON.stringify(errors)}`,
			);
		}
		for (const value of ['E111', 'RUF055', 'undefined-name', 'correctness', 'ANN101', 'XYZ', '']) {
			const errors = await service.doValidation(
				document(JSON.stringify(config(key, ['F821', value], override))),
				false,
			);
			assert.ok(errors.length > 0, `${key}=${value}, override=${override}: unsupported selector accepted`);
		}
	}
}
console.log('YAML language service: safe selector completions, upstream Python targets, and validation passed');
