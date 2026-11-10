import assert from 'node:assert/strict';

import { readFile } from 'node:fs/promises';

import { TextDocument } from 'vscode-languageserver-textdocument';
import { getLanguageService } from 'yaml-language-server/out/src/languageservice/yamlLanguageService.js';

// VS Code URI normalization lowercases Windows drive letters.
/** @param {string} uri */
const schemaKey = (uri) => uri.replace(/^file:\/\/\/([A-Z]):/, (prefix) => prefix.toLowerCase());
const schemaURI = new URL('../../../../actionlint.schema.json', import.meta.url).href;
/** @type {Map<string, string>} */
const schemas = new Map();
for (const href of [schemaURI, import.meta.resolve('#schemas/shellcheck/0.11.0.schema.json')]) {
	schemas.set(schemaKey(href), await readFile(new URL(href), 'utf8'));
}

/** @param {string} uri */
function schemaSource(uri) {
	const source = schemas.get(schemaKey(uri));
	assert.ok(source !== undefined, `Unexpected schema request: ${uri}`);
	return source;
}

/** @param {unknown} value @returns {Record<string, unknown>} */
function schemaObject(value) {
	assert.ok(value !== null && typeof value === 'object' && !Array.isArray(value), 'Expected a schema object');
	return Object.fromEntries(Object.entries(value));
}

/** @typedef {{const: string, deprecated?: boolean, description?: string}} RuleNameChoice */
/** @param {unknown} value @returns {RuleNameChoice} */
function ruleNameChoice(value) {
	const entry = schemaObject(value);
	const name = entry.const;
	const deprecated = entry.deprecated;
	const description = entry.description;
	assert.ok(typeof name === 'string', 'Expected a rule name');
	assert.ok(deprecated === undefined || typeof deprecated === 'boolean', 'Expected a deprecation flag');
	assert.ok(description === undefined || typeof description === 'string', 'Expected a rule description');
	return { const: name, deprecated, description };
}
// An optional snapshot lets the same editor checks compare generated revisions.
if (process.argv[2]) schemas.set(schemaKey(schemaURI), await readFile(process.argv[2], 'utf8'));
const service = getLanguageService({
	workspaceContext: {
		/** @param {string} relative @param {string} resource */
		resolveRelativePath: (relative, resource) => new URL(relative, resource).href,
	},
	/** @param {string} uri */
	schemaRequestService: async (uri) => schemaSource(uri),
});
service.configure({
	completion: true,
	hover: true,
	validate: true,
	yamlVersion: '1.2',
	schemas: [{ uri: schemaURI, fileMatch: ['*'] }],
});
let serial = 0;
/** @param {string} text */
const document = (text) => TextDocument.create(`file:///review/schema-completion-${serial++}.yaml`, 'yaml', 1, text);

/** @type {unknown} */
const schema = JSON.parse(schemaSource(schemaURI));
const definitions = schemaObject(schemaObject(schema).$defs);
const choices = schemaObject(definitions['known-rule-name']).anyOf;
assert.ok(Array.isArray(choices), 'Expected rule-name completion choices');
const nameChoices = choices.map(ruleNameChoice);
const knownRules = nameChoices.map(choice => choice.const);
const oldTimeout = nameChoices.find(choice => choice.const === 'require-job-timeout');
assert.equal(oldTimeout?.deprecated, true, 'Historical rule alias must be deprecated');
assert.ok(oldTimeout?.description?.includes('job-timeout'), 'Deprecated alias must name its replacement');
assert.ok(
	!nameChoices.find(choice => choice.const === 'job-timeout')?.deprecated,
	'Current rule must not be deprecated',
);
const deprecatedSelector = document('lint:\n  rules:\n    disable: [require-job-timeout]\n');
const deprecatedHover = await service.doHover(deprecatedSelector, { line: 2, character: 19 });
assert.ok(
	JSON.stringify(deprecatedHover).includes('Deprecated alias. Use job-timeout instead.'),
	'Alias hover lost its deprecation and replacement',
);
for (const name of ['job-timeout', 'unpublished-custom-check']) {
	const current = document(`lint:\n  rules:\n    disable: [${name}]\n`);
	const hover = await service.doHover(current, { line: 2, character: 19 });
	assert.ok(!JSON.stringify(hover).includes('Deprecated alias.'), `Deprecation leaked into ${name}`);
}
/** @param {unknown} node @returns {Record<string, unknown>[]} */
function aliasSettings(node) {
	if (Array.isArray(node)) return node.flatMap(aliasSettings);
	if (!node || typeof node !== 'object') return [];
	return Object.entries(schemaObject(node)).flatMap(([key, value]) =>
		key === 'require-job-timeout' ? [schemaObject(value)] : aliasSettings(value)
	);
}
const deprecatedKeys = aliasSettings(schema);
assert.equal(deprecatedKeys.length, 2, 'Alias keys must exist at root and override scope');
assert.ok(
	deprecatedKeys.every(key => key.deprecated === true),
	'Alias keys must carry their own deprecation annotation',
);
console.log('Deprecated alias values and keys identify their replacement; current and custom names are not deprecated');
assert.equal(new Set(knownRules).size, knownRules.length, 'Known rule names must be unique');
if (process.argv[3]) assert.ok(knownRules.includes(process.argv[3]), 'Installed rule missing from suggestions');
for (const override of [false, true]) {
	for (
		const [selector, entry] of Object.entries({
			ignore: 'ignores:\n  - file: "**"\n    rule: ',
			disable: 'lint:\n  rules:\n    disable:\n      - ',
			'rule-options':
				'lint:\n  rules:\n    conventions:\n      forbidden-suppression:\n        level: on\n        options:\n          rules:\n            - ',
		})
	) {
		if (override && selector === 'ignore') continue; // Ignores are declared at the root.
		const text = override
			? `overrides:\n  - includes: ["**"]\n${entry.split('\n').map(line => `    ${line}`).join('\n')}`
			: entry;
		const doc = document(text);
		const result = await service.doComplete(doc, doc.positionAt(text.length), false);
		const labels = result?.items.map(item => item.label.replaceAll('"', '')) ?? [];
		for (const name of knownRules) {
			assert.equal(
				labels.filter(label => label === name).length,
				1,
				`${selector}, override=${override}: missing or duplicate ${name}`,
			);
		}
		for (const name of ['expression', 'unpublished-custom-check']) {
			assert.deepEqual(await service.doValidation(document(text + name), false), [], `${selector}: rejected ${name}`);
		}
		for (const value of ['""', '42']) {
			assert.ok(
				(await service.doValidation(document(text + value), false)).length > 0,
				`${selector}: accepted ${value}`,
			);
		}
		console.log(
			`Rule selector ${selector}, override=${override}: ${knownRules.length} completions; custom names accepted`,
		);
	}
}
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
		['lint:\n  rules:\n    conventions:\n      "r|": on\n', 'job-timeout'],
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

for (const override of [false, true]) {
	for (const groupValue of [false, true]) {
		const prefix = override
			? 'overrides:\n  - includes: ["**"]\n    lint:\n      rules:\n        '
			: 'lint:\n  rules:\n    ';
		const text = prefix
			+ (groupValue ? 'conventions: ' : `conventions:\n${override ? '          ' : '      '}job-timeout: `);
		const doc = document(text);
		const result = await service.doComplete(doc, doc.positionAt(text.length), false);
		assert.ok(result);
		const labels = result.items.map(item => item.label.replaceAll('"', ''));
		for (const level of ['default', 'off', 'on', 'info', 'warn', 'error', 'true', 'false']) {
			assert.ok(labels.includes(level), `${text}: missing ${level}: ${JSON.stringify(labels)}`);
		}
		assert.equal(labels.includes('all'), groupValue, `${text}: incorrect group-only preset suggestion`);
		console.log(`Rule completions: ${JSON.stringify({ override, groupValue, labels: labels.sort() })}`);
	}
}
const namedRule = document('lint:\n  rules:\n    conventions:\n      job-timeout: on\n');
const hover = await service.doHover(namedRule, { line: 3, character: 10 });
console.log(`Rule hover: ${JSON.stringify(hover)}`);
assert.ok(JSON.stringify(hover).includes('Require explicit job timeouts'), 'Shared refs lost the rule-specific hover');
console.log('Rule levels, group-only all and rule-specific hover passed at root and override scope');
const dotted = document('lint.rules.security: on\n');
assert.ok(
	(await service.doValidation(dotted, false)).length > 0,
	'Dotted YAML keys are not nested configuration paths',
);
console.log(
	'Quoted YAML key/value prefixes verified; dotted YAML property paths are unsupported syntax. Use nested mappings or TOML dotted keys.',
);

for (const quote of ['"', "'"]) {
	for (
		const [text, expected, label] of [
			[
				`lint:\n  rules:\n    ${quote}|${quote}: on\n`,
				`lint:\n  rules:\n    ${quote}security${quote}: on\n`,
				'security',
			],
			[
				`lint:\n  rules:\n    ${quote}s|${quote}: on\n`,
				`lint:\n  rules:\n    ${quote}security${quote}: on\n`,
				'security',
			],
			[
				`lint:\n  rules:\n    security: ${quote}|${quote}\n`,
				`lint:\n  rules:\n    security: ${quote}on${quote}\n`,
				'on',
			],
			[
				`lint:\n  rules:\n    security: ${quote}o|${quote}\n`,
				`lint:\n  rules:\n    security: ${quote}on${quote}\n`,
				'on',
			],
			[`lint:\n  rules:\n    security: ${quote}o|\n`, `lint:\n  rules:\n    security: ${quote}on${quote}\n`, 'on'],
			[`lint: {rules: {${quote}|${quote}: on}}\n`, `lint: {rules: {${quote}security${quote}: on}}\n`, 'security'],
			[`lint: {rules: {security: ${quote}|${quote}}}\n`, `lint: {rules: {security: ${quote}on${quote}}}\n`, 'on'],
		]
	) {
		const offset = text.indexOf('|');
		const doc = document(text.replace('|', ''));
		const completion = await service.doComplete(doc, doc.positionAt(offset), false);
		const item = completion?.items.find(candidate => candidate.label.replaceAll('"', '') === label);
		assert.ok(item?.textEdit && 'range' in item.textEdit, `${text}: missing ${label}`);
		const applied = TextDocument.applyEdits(doc, [item.textEdit]);
		assert.equal(applied, expected, `${text}: completion must preserve existing quotes`);
		assert.deepEqual(
			await service.doValidation(document(applied), false),
			[],
			`${text}: completion produces invalid YAML`,
		);
		console.log(`Quoted completion ${JSON.stringify(text)} produces ${JSON.stringify(applied)}`);
	}
}
