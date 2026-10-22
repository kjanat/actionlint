import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';

import ts from 'typescript';

const require = createRequire(import.meta.url);
const manifest = require.resolve('@octokit/openapi-webhooks-types/package.json');
const pkg = JSON.parse(readFileSync(manifest, 'utf8')) as { name: string; version: string };
const input = resolve(dirname(manifest), 'types.d.ts');
const program = ts.createProgram([input], {
	strict: true,
	skipLibCheck: true,
	types: [],
	target: ts.ScriptTarget.ESNext,
});
const checker = program.getTypeChecker();
const sourceFile = program.getSourceFile(input);
if (!sourceFile) throw new Error(`Missing source file: ${input}`);
const source = sourceFile;
const errors = ts.getPreEmitDiagnostics(program);
if (errors.length) {
	throw new Error(ts.formatDiagnosticsWithColorAndContext(errors, {
		getCurrentDirectory: () => process.cwd(),
		getCanonicalFileName: x => x,
		getNewLine: () => '\n',
	}));
}
const operations = source.statements.find(s => ts.isInterfaceDeclaration(s) && s.name.text === 'operations');
if (!operations) throw new Error('Missing OpenAPI operations');

type Property = { type: number; optional: boolean; description?: string };
type Shape = {
	kind: string;
	properties?: Record<string, Property>;
	element?: number;
	mapped?: number;
	alternatives?: number[];
	literal?: string;
};
const nodes: Shape[] = [];
const seen = new Map<ts.Type, number>();
function convert(type: ts.Type): number {
	const known = seen.get(type);
	if (known !== undefined) return known;
	const id = nodes.length;
	const node: Shape = { kind: 'any' };
	nodes.push(node);
	seen.set(type, id);
	if (type.flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown)) return id;
	if (type.flags & (ts.TypeFlags.Never | ts.TypeFlags.Undefined)) node.kind = 'absent';
	else if (type.flags & ts.TypeFlags.Null) node.kind = 'null';
	else if (type.flags & ts.TypeFlags.StringLike) {
		node.kind = 'string';
		if (type.isStringLiteral()) node.literal = type.value;
	} else if (type.flags & ts.TypeFlags.NumberLike) node.kind = 'number';
	else if (type.flags & ts.TypeFlags.BooleanLike) node.kind = 'boolean';
	else if (type.isUnion()) {
		node.kind = 'union';
		node.alternatives = type.types.map(convert);
	} else if (checker.isArrayType(type)) {
		node.kind = 'array';
		const [element] = checker.getTypeArguments(type as ts.TypeReference);
		if (!element) throw new Error(`Missing array element type: ${checker.typeToString(type)}`);
		node.element = convert(element);
	} else if (type.flags & (ts.TypeFlags.Object | ts.TypeFlags.Intersection)) {
		node.kind = 'object';
		node.properties = {};
		for (const property of type.getProperties().sort((a, b) => a.name.localeCompare(b.name, 'en'))) {
			const description = ts.displayPartsToString(property.getDocumentationComment(checker));
			node.properties[property.name] = {
				type: convert(checker.getTypeOfSymbolAtLocation(property, property.valueDeclaration ?? source)),
				optional: !!(property.flags & ts.SymbolFlags.Optional),
				...(description ? { description } : {}),
			};
		}
		const mapped = type.getStringIndexType();
		if (mapped) node.mapped = convert(mapped);
	} else throw new Error(`Unsupported payload type: ${checker.typeToString(type)}`);
	return id;
}
function property(type: ts.Type, key: string): ts.Type {
	const symbol = type.getProperty(key);
	if (!symbol) throw new Error(`Missing ${key} in ${checker.typeToString(type)}`);
	return checker.getTypeOfSymbolAtLocation(symbol, symbol.valueDeclaration ?? source);
}
const events: Record<string, Record<string, number>> = {};
for (
	const operation of checker.getTypeAtLocation(operations).getProperties().sort((a, b) =>
		a.name.localeCompare(b.name, 'en')
	)
) {
	const [event = '', action = ''] = operation.name.split('/');
	const name = event.replaceAll('-', '_');
	const type = checker.getTypeOfSymbolAtLocation(operation, operation.valueDeclaration ?? source);
	const payload = property(property(property(type, 'requestBody'), 'content'), 'application/json');
	events[name] ??= {};
	events[name][action.replaceAll('-', '_')] = convert(payload);
}
const catalogue = {
	source: {
		package: pkg.name,
		version: pkg.version,
		sha256: createHash('sha256').update(readFileSync(input)).digest('hex'),
		url: 'https://github.com/octokit/openapi-webhooks',
	},
	events,
	nodes,
};
const output = resolve(import.meta.dirname, '../catalogue.json');
// Format before writing so regeneration never relies on a subsequent autofix.
const formatted = execFileSync('dprint', ['fmt', '--stdin', 'catalogue.json'], {
	input: JSON.stringify(catalogue),
	maxBuffer: 32 << 20,
});
writeFileSync(output, formatted);
console.log(
	`Generated ${Object.keys(events).length} events and ${nodes.length} payload shapes from ${pkg.name}@${pkg.version}`,
);
