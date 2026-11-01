import assert from 'node:assert/strict';

import { resolve } from 'node:path';

import { test } from 'bun:test';
import ts from 'typescript';

test('SDK source tooltips explain provider options at author call sites', () => {
	const directory = import.meta.dirname;
	const filename = resolve(directory, 'editor-hover.fixture.ts').replaceAll('\\', '/');
	const source = `
import { defineDocumentProvider, defineScriptProvider, defineProcessProvider, type ProviderIdentity } from '../src/index.ts';
const identity: ProviderIdentity = {
  id: 'example', version: '1.0.0', name: 'Example', description: 'Example', license: 'MIT',
  repository: 'https://github.com/example/plugin',
  rule: { name: 'example', category: 'style', subcategory: 'yaml', maturity: 'stable', description: 'Example', provider: 'example' }
};
const document = defineDocumentProvider({ ...identity, env_passthrough: ['PATH'], check: () => [] });
const script = defineScriptProvider({ ...identity, env_passthrough: ['PATH'], shells: ['bash'], check: () => [] });
const native = defineProcessProvider({ ...identity, env_passthrough: ['PATH'], shells: ['bash'], entry: 'tool', stdin: 'source', config: { render: 'none' }, output: { format: 'script-diagnostics@1' } });
const nativeDocument = defineProcessProvider({ ...identity, env_passthrough: ['PATH'], role: 'document', entry: 'tool', stdin: 'request', config: { render: 'none' }, output: { format: 'sarif@2.1.0' } });
document.env_passthrough;
script.env_passthrough;
native.env_passthrough;
nativeDocument.env_passthrough;
document.documentKinds;
document.rule.name;
`;
	const matches = (file: string) => resolve(file).replaceAll('\\', '/').toLowerCase() === filename.toLowerCase();
	const options = {
		strict: true,
		noEmit: true,
		allowImportingTsExtensions: true,
		target: ts.ScriptTarget.ESNext,
		module: ts.ModuleKind.ESNext,
		moduleResolution: ts.ModuleResolutionKind.Bundler,
	};
	const service = ts.createLanguageService({
		getScriptFileNames: () => [filename],
		getScriptVersion: () => '0',
		getScriptSnapshot: (file) => {
			const content = matches(file) ? source : ts.sys.readFile(file);
			return content === undefined ? undefined : ts.ScriptSnapshot.fromString(content);
		},
		getCurrentDirectory: () => directory,
		getCompilationSettings: () => options,
		getDefaultLibFileName: ts.getDefaultLibFilePath,
		fileExists: (file) => matches(file) || ts.sys.fileExists(file),
		readFile: (file) => matches(file) ? source : ts.sys.readFile(file),
		readDirectory: ts.sys.readDirectory,
	});
	try {
		assert.deepEqual(service.getSemanticDiagnostics(filename), []);
		const documentation = (position: number) => {
			assert.ok(position >= 0, 'hover target missing');
			const info = service.getQuickInfoAtPosition(filename, position + 1);
			assert.ok(info, 'TypeScript returned no tooltip');
			return ts.displayPartsToString(info.documentation);
		};
		for (const name of ['document', 'script', 'native', 'nativeDocument']) {
			const input = source.indexOf('env_passthrough', source.indexOf(`const ${name} =`));
			const result = source.indexOf(`${name}.env_passthrough`) + name.length + 1;
			for (const position of [input, result]) {
				const text = documentation(position);
				assert.match(text, /Host environment variable names/, `${name} hover at ${position}`);
				assert.match(text, /Defaults to none/);
				assert.match(text, /launches another installed command/);
			}
		}
		assert.match(documentation(source.indexOf('document.documentKinds') + 'document.'.length), /Defaults to both/);
		assert.match(
			documentation(source.indexOf('document.rule.name') + 'document.rule.'.length),
			/Stable rule\/config key/,
		);
	} finally {
		service.dispose();
	}
});
