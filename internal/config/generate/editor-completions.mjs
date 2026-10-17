import assert from 'node:assert/strict';

import { readFile } from 'node:fs/promises';

import { TextDocument } from 'vscode-languageserver-textdocument';
import { getLanguageService } from 'yaml-language-server/out/src/languageservice/yamlLanguageService.js';

const schemaURI = new URL('../../../actionlint.schema.json', import.meta.url).href;
const schemas = new Map(
	await Promise.all([
		schemaURI,
		import.meta.resolve('#schemas/shellcheck/0.11.0.schema.json'),
	].map(async (href) => /** @type {const} */ ([href, await readFile(new URL(href), 'utf8')]))),
);
const service = getLanguageService({
	workspaceContext: {
		/** @param {string} relative @param {string} resource */
		resolveRelativePath: (relative, resource) => new URL(relative, resource).href,
	},
	/** @param {string} uri */
	schemaRequestService: async (uri) => {
		const source = schemas.get(uri);
		assert.ok(source !== undefined, `Unexpected schema request: ${uri}`);
		return source;
	},
});
service.configure({
	completion: true,
	validate: true,
	yamlVersion: '1.2',
	schemas: [{ uri: schemaURI, fileMatch: ['*'] }],
});
let serial = 0;
/** @param {string} text */
const document = (text) => TextDocument.create(`file:///review/ruff-${serial++}.yaml`, 'yaml', 1, text);
/** @param {string} key @param {string[] | null} value @param {boolean} override */
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
		const lastLine = lines.at(-1);
		assert.ok(lastLine !== undefined);
		/** @type {{ items: { label: string }[] } | null} */
		const result = await service.doComplete(document(`${text}\n`), {
			line: lines.length - 1,
			character: lastLine.length,
		}, false);
		assert.ok(result);
		const labels = result.items.map((item) => item.label);
		if (key === 'target-version') {
			assert.deepEqual(
				labels.filter((label) => label !== 'null').sort(),
				['py314'],
			);
		} else {
			assert.deepEqual(labels.sort(), ['F', 'F821'], `${key}, override=${override}: unsafe or missing completions`);
		}
	}
	for (const key of ['select', 'ignore']) {
		for (const value of [null, [], ['F821'], ['C9', 'U004', 'PGH001'], ['E111', 'RUF055', 'ENGINE999']]) {
			const errors = await service.doValidation(document(JSON.stringify(config(key, value, override))), false);
			assert.equal(
				errors.length,
				0,
				`${key}=${JSON.stringify(value)}, override=${override}: ${JSON.stringify(errors)}`,
			);
		}
		for (const value of ['']) {
			const errors = await service.doValidation(
				document(JSON.stringify(config(key, ['F821', value], override))),
				false,
			);
			assert.ok(errors.length > 0, `${key}=${value}, override=${override}: unsupported selector accepted`);
		}
	}
}
console.log(
	'YAML language service: safe selector completions, engine-independent targets, and wrapper validation passed',
);
