import assert from 'node:assert/strict';

import { readFile } from 'node:fs/promises';

import { TextDocument } from 'vscode-languageserver-textdocument';
import { getLanguageService } from 'yaml-language-server/out/src/languageservice/yamlLanguageService.js';

// VS Code URI normalization lowercases Windows drive letters.
/** @param {string} uri */
const schemaKey = (uri) => uri.replace(/^file:\/\/\/([A-Z]):/, (prefix) => prefix.toLowerCase());
const schemaURI = new URL('../../actionlint.schema.json', import.meta.url).href;
const schemas = new Map(
	await Promise.all([
		schemaURI,
		import.meta.resolve('#schemas/shellcheck/0.11.0.schema.json'),
	].map(async (href) => /** @type {const} */ ([schemaKey(href), await readFile(new URL(href), 'utf8')]))),
);
const service = getLanguageService({
	workspaceContext: {
		/** @param {string} relative @param {string} resource */
		resolveRelativePath: (relative, resource) => new URL(relative, resource).href,
	},
	/** @param {string} uri */
	schemaRequestService: async (uri) => {
		const source = schemas.get(schemaKey(uri));
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

// Cursor markers exercise the original quoted-key and quoted-value editor cases.
for (
	const [text, expected] of [
		['lint:\n  rules:\n    "s|": on\n', 'security'],
		['lint:\n  rules:\n    policy:\n      "r|": on\n', 'require-commit-hash'],
		['tools:\n  shellcheck:\n    config:\n      enable: "a|"\n', 'all'],
		['tools:\n  ruff:\n    select: ["|F"]\n', 'F821'],
	]
) {
	const offset = text.indexOf('|');
	const doc = document(text.replace('|', ''));
	const result = await service.doComplete(doc, doc.positionAt(offset), false);
	const item = result?.items.find((candidate) => candidate.label.replaceAll('"', '') === expected);
	assert.ok(item?.textEdit && 'range' in item.textEdit, `${text}: missing ${expected}: ${JSON.stringify(result)}`);
	const completed = document(TextDocument.applyEdits(doc, [item.textEdit]));
	assert.deepEqual(await service.doValidation(completed, false), [], `${text}: completion produces invalid YAML`);
	console.log(`Completion ${JSON.stringify(text)} includes ${expected}; applied edit validates`);
}
const dottedPrefix = document('lint.rules.');
const dottedResult = await service.doComplete(
	dottedPrefix,
	dottedPrefix.positionAt(dottedPrefix.getText().length),
	false,
);
assert.ok(
	!dottedResult?.items.some((item) => item.label === 'security'),
	'Dotted YAML keys must not advertise unsupported nested paths',
);
console.log(`Completion after lint.rules.: ${JSON.stringify(dottedResult?.items.map((item) => item.label) ?? [])}`);
const dotted = document('lint.rules.security: on\n');
assert.ok(
	(await service.doValidation(dotted, false)).length > 0,
	'Dotted YAML keys are not nested configuration paths',
);
console.log(
	'Quoted YAML key/value prefixes verified; dotted YAML property paths are unsupported syntax. Use nested mappings or TOML dotted keys.',
);

const emptyKey = document('lint:\n  rules:\n    "": on\n');
const emptyKeyResult = await service.doComplete(emptyKey, { line: 2, character: 5 }, false);
console.log(
	`YAML LS empty quoted key limitation: ${
		emptyKeyResult?.items.length ?? 0
	} suggestions; entering a key prefix restores completion.`,
);

const emptyValue = document('lint:\n  rules:\n    security: ""\n');
const valueResult = await service.doComplete(emptyValue, { line: 2, character: 15 }, false);
const on = valueResult?.items.find((item) => item.label.replaceAll('"', '') === 'on');
assert.ok(on?.textEdit && 'range' in on.textEdit, 'Missing quoted diagnostic-level suggestion');
const appliedValue = document(TextDocument.applyEdits(emptyValue, [on.textEdit]));
console.log(
	`YAML LS empty quoted level edit: ${JSON.stringify(appliedValue.getText())}; diagnostics: ${
		JSON.stringify(await service.doValidation(appliedValue, false))
	}`,
);
