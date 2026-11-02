import { spawnSync } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { expect, test } from 'bun:test';

import { type DocumentRequest, executeDocumentCheck, executeScriptCheck, unicodeColumn } from '#index';

const sdkRoot = resolve(import.meta.dirname, '..');
const cli = join(sdkRoot, 'src', 'cli.ts');
const sdkSource = JSON.stringify(join(sdkRoot, 'src', 'index.ts').replaceAll('\\', '/'));
const identity =
	`id:'test/example',version:'1.0.0',name:'Example',description:'Example',license:'MIT',repository:'https://github.com/example/plugin',rule:{name:'example',category:'correctness',subcategory:'workflow',maturity:'stable',description:'Example',provider:'test/example'}`;

test('rejects malformed JSON requests without calling the check', async () => {
	let called = false;
	const result = await executeScriptCheck(() => {
		called = true;
		return [];
	}, '{}');
	expect(called).toBe(false);
	expect(result.ok).toBe(false);
	expect(result.errors).toEqual(['expected {source, filename, config?}']);
});

test('reports callback exceptions in the response envelope', async () => {
	expect(
		await executeScriptCheck(() => {
			throw new Error('check failed');
		}, JSON.stringify({ source: 'x', filename: 'x.sh' })),
	)
		.toEqual({ ok: false, diagnostics: [], errors: ['check failed'] });
});

test('native directives remain source text and callbacks own their suppression decisions', async () => {
	const source = '# native-linter: disable X1\n# actionlint-ignore example\ncheck()\n';
	const input = JSON.stringify({ source, filename: 'snippet.txt' });
	const reported = await executeScriptCheck(request => {
		expect(request.source).toBe(source);
		return [{ filename: request.filename, code: 'X1', message: 'active finding', location: { row: 3, column: 1 } }];
	}, input);
	expect(reported.ok).toBe(true);
	expect(reported.diagnostics).toHaveLength(1);
	const suppressed = await executeScriptCheck(request => {
		expect(request.source).toBe(source);
		return [];
	}, input);
	expect(suppressed).toEqual({ ok: true, diagnostics: [], errors: [] });
});

test('rejects invalid fix ranges', async () => {
	const output = await executeScriptCheck(({ filename }) => [{
		filename,
		code: 'BAD',
		message: 'bad fix',
		location: { row: 1, column: 1 },
		fixes: [{ replacements: [{ row: 2, column: 1, end_row: 1, end_column: 1, text: '' }] }],
	}], JSON.stringify({ source: 'x', filename: 'x.sh' }));
	expect(output).toEqual({ ok: false, diagnostics: [], errors: ['checker returned invalid diagnostics'] });
});

test('bounds request and response bytes', async () => {
	const input = JSON.stringify({ source: 'long', filename: 'x' });
	expect((await executeScriptCheck(() => [], input, 100, 3)).errors).toEqual(['request exceeds 3 byte limit']);
	expect(
		(await executeScriptCheck(
			({ filename }) => [{ filename, code: 'X', message: 'long', location: { row: 1, column: 1 } }],
			input,
			10,
		)).errors,
	)
		.toEqual(['response exceeds 10 byte limit']);
});

test('counts Unicode scalar columns', () => {
	expect(unicodeColumn('é🙂z', 4)).toBe(3);
});

test('document callbacks receive only the public DTO', async () => {
	const request: DocumentRequest = {
		source: 'jobs: {}',
		filename: 'workflow.yml',
		kind: 'workflow',
		config: { enabled: true },
		directory: '/workspace',
		dependencies: [{
			reference: { document: 'workflow.yml', revision: 'r1', node: 'step', address: '/jobs/build/steps/0/uses' },
			uses: 'actions/checkout@v4',
			kind: 'action',
			start: { line: 4, column: 9 },
			end: { line: 4, column: 37 },
		}],
	};
	let received: DocumentRequest | undefined;
	expect(
		(await executeDocumentCheck((value) => {
			received = value;
			return [];
		}, JSON.stringify({ ...request, fileHandle: { opaque: true } }))).ok,
	).toBe(true);
	expect(received).toEqual(request);
	let called = false;
	const invalid = await executeDocumentCheck(
		() => {
			called = true;
			return [];
		},
		JSON.stringify({
			source: 'x',
			filename: 'x.yml',
			kind: 'workflow',
			dependencies: [{ start: { line: 3, column: 1 }, end: { line: 2, column: 1 } }],
		}),
	);
	expect(called).toBe(false);
	expect(invalid.ok).toBe(false);
});

test('document callback failures and byte limits produce protocol failures', async () => {
	const input = JSON.stringify({ source: 'x', filename: 'x.yml', kind: 'action' });
	expect(
		await executeDocumentCheck(() => {
			throw new Error('document failed');
		}, input),
	)
		.toEqual({ ok: false, diagnostics: [], errors: ['document failed'] });
	expect(
		(await executeDocumentCheck(
			({ filename }) => [{ filename, code: 'LARGE', message: 'large', location: { row: 1, column: 1 } }],
			input,
			10,
		)).errors,
	)
		.toEqual(['response exceeds 10 byte limit']);
});

test('source CLI embeds metadata and jq settings/output programs', async () => {
	await withProvider(
		`import {defineProcessProvider,jqOutput} from ${sdkSource};
export default defineProcessProvider({${identity},shells:['bash'],entry:'tool',stdin:'request',config:{render:'json-stdin',transform:{language:'jq',inline:'{enabled:(.enabled // true)}'}},output:{format:'json',transform:jqOutput('./output.jq')}});`,
		async (root) => {
			await writeFile(join(root, 'output.jq'), '.');
			const manifest = await pack(root);
			expect(manifest).toMatchObject({
				schema_version: 1,
				id: 'test/example',
				version: '1.0.0',
				metadata: { id: 'test/example', plugin_version: '1.0.0', transport: 'process' },
				process: {
					config: { render: 'json-stdin', transform: { language: 'jq', inline: '{enabled:(.enabled // true)}' } },
					output: { format: 'json', transform: { language: 'jq', inline: '.' } },
				},
			});
		},
	);
});

test('Bun packs and installs a self-contained script provider with Unicode fixes', async () => {
	await withProvider(
		`import {defineScriptProvider} from ${sdkSource};
import {spawnSync} from 'node:child_process';
export default defineScriptProvider({${identity},shells:['bash'],fixes:true,env_passthrough:['PATH','SystemRoot','PATHEXT'],check:({source,filename,config})=>{
 const child=spawnSync('bun',['--version'],{encoding:'utf8'});if(child.status!==0)throw new Error('Bun unavailable through declared PATH');
 if(typeof config==='object'&&config!==null&&!Array.isArray(config)&&config.enabled===false)return [];
 const line=source.split('\\n')[0];const trimmed=line.trimEnd();if(line===trimmed)return [];
 const column=[...trimmed].length+1;const end=[...line].length+1;
 return [{filename,code:'TRAILING_SPACE',message:child.stdout.trim(),severity:'warning',location:{row:1,column},end_location:{row:1,column:end},fixes:[{replacements:[{row:1,column,end_row:1,end_column:end,text:''}]}]}];}});`,
		async (root) => {
			await writeFile(
				join(root, 'package.json'),
				JSON.stringify({
					name: 'actionlint-sdk-test',
					version: '1.0.0',
					type: 'module',
					exports: { './existing': './existing.js' },
					files: ['README.md'],
				}),
			);
			await writeFile(join(root, 'README.md'), 'test package\n');
			const manifest = await pack(root);
			expect(manifest).toMatchObject({
				selectors: { shells: ['bash'] },
				process: { entry: 'node', args: ['{program-file}'], config: { render: 'json-stdin' } },
			});
			expect(await jsonFile(join(root, 'package.json'))).toMatchObject({
				exports: { './existing': './existing.js', './actionlint': './actionlint.js' },
				files: ['README.md', 'actionlint.js', 'actionlint-provider.json'],
			});
			await pack(root);
			const consumer = await installPackage(root);
			const output = await executeEmbedded(root, manifest, consumer, {
				source: 'echo 🙂  \n',
				filename: 'script.sh',
				config: { enabled: true },
			});
			expect(output).toMatchObject({
				ok: true,
				diagnostics: [{
					code: 'TRAILING_SPACE',
					location: { row: 1, column: 7 },
					end_location: { row: 1, column: 9 },
					fixes: [{ replacements: [{ row: 1, column: 7, end_row: 1, end_column: 9, text: '' }] }],
				}],
			});
			expect(
				await executeEmbedded(root, manifest, consumer, {
					source: 'echo 🙂  \n',
					filename: 'script.sh',
					config: { enabled: false },
				}),
			).toMatchObject({ ok: true, diagnostics: [] });
			const installed = spawnSync(
				process.execPath,
				[join(consumer, 'node_modules', 'actionlint-sdk-test', 'actionlint.js')],
				{
					cwd: consumer,
					input: JSON.stringify({ source: 'echo 🙂  \n', filename: 'script.sh' }),
					encoding: 'utf8',
				},
			);
			expect(installed.status, installed.stderr).toBe(0);
			expect(parseJSON(installed.stdout)).toEqual(output);
		},
	);
});

test('raw document recipes preserve selectors and requirements', async () => {
	await withProvider(
		`import {defineProcessProvider} from ${sdkSource};
export default defineProcessProvider({${identity},role:'document',documentKinds:['action'],requires:['source','dependencies'],entry:'linter',stdin:'request',config:{render:'json-stdin'},output:{format:'script-diagnostics@1'}});`,
		async (root) => {
			expect(await pack(root)).toMatchObject({
				role: 'document',
				preparation: 'generic',
				selectors: { documents: ['action'] },
				requires: ['source', 'dependencies'],
				process: { entry: 'linter' },
			});
			expect(await Bun.file(join(root, 'actionlint.js')).exists()).toBe(false);
		},
	);
});

test('packed document callback receives config/dependencies outside the author project', async () => {
	await withProvider(
		`import {defineDocumentProvider} from ${sdkSource};
export default defineDocumentProvider({${identity},requires:['dependencies'],resources:['directory'],check:(request)=>{
 const config=request.config;if(typeof config==='object'&&config!==null&&!Array.isArray(config)&&config.fail===true)throw new Error('document failed');
 const dependency=request.dependencies?.[0];return dependency?[{filename:request.filename,code:'DOC',message:request.kind+'/'+request.directory+'/'+dependency.reference.address+'/'+dependency.start.line,location:{row:1,column:1}}]:[];}});`,
		async (root) => {
			const manifest = await pack(root);
			const consumer = await installPackage(root);
			const request = {
				source: 'jobs: {}',
				filename: 'workflow.yml',
				kind: 'workflow',
				directory: '/workspace',
				config: { enabled: true },
				dependencies: [{
					reference: { document: 'workflow.yml', revision: 'r1', node: 'step', address: '/jobs/build/steps/0/uses' },
					uses: 'owner/action@v1',
					kind: 'action',
					start: { line: 4, column: 9 },
					end: { line: 4, column: 30 },
				}],
			};
			expect(await executeEmbedded(root, manifest, consumer, request)).toMatchObject({
				ok: true,
				diagnostics: [{ code: 'DOC', message: 'workflow//workspace//jobs/build/steps/0/uses/4' }],
			});
			expect(await executeEmbedded(root, manifest, consumer, { ...request, config: { fail: true } })).toEqual({
				ok: false,
				diagnostics: [],
				errors: ['document failed'],
			});
		},
	);
});

test('tsdown bundles YAML into a standalone JavaScript program', async () => {
	const yaml = JSON.stringify(Bun.resolveSync('yaml', sdkRoot).replaceAll('\\', '/'));
	await withProvider(
		`import YAML from ${yaml};import {defineDocumentProvider} from ${sdkSource};
export default defineDocumentProvider({${identity},bundle:{platform:'node'},check:({source,filename})=>YAML.parse(source)?.name==='sample'?[{filename,code:'YAML_OK',message:'bundled YAML',location:{row:1,column:1}}]:[]});`,
		async (root) => {
			const manifest = await pack(root);
			const consumer = join(root, 'isolated');
			await mkdir(consumer);
			expect(
				await executeEmbedded(root, manifest, consumer, {
					source: 'name: sample\n',
					filename: 'workflow.yml',
					kind: 'workflow',
				}),
			).toMatchObject({ ok: true, diagnostics: [{ code: 'YAML_OK' }] });
		},
	);
});

test('source CLI initializes a TypeScript provider without overwriting files', async () => {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-sdk-init-'));
	try {
		expect(runCLI(['init', root]).status).toBe(0);
		await writeFile(join(root, 'actionlint.plugin.ts'), '// author content\n');
		expect(runCLI(['init', root]).status).not.toBe(0);
		expect(await readFile(join(root, 'actionlint.plugin.ts'), 'utf8')).toBe('// author content\n');
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});

async function withProvider(source: string, check: (root: string) => Promise<void>): Promise<void> {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-sdk-source-'));
	try {
		await writeFile(
			join(root, 'package.json'),
			JSON.stringify({ name: 'actionlint-sdk-test', version: '1.0.0', type: 'module' }),
		);
		await writeFile(join(root, 'actionlint.plugin.ts'), source);
		await check(root);
	} finally {
		await rm(root, { recursive: true, force: true });
	}
}

function runCLI(args: string[]) {
	return spawnSync(process.execPath, [cli, ...args], { encoding: 'utf8' });
}

async function pack(root: string): Promise<unknown> {
	const result = runCLI(['pack', join(root, 'actionlint.plugin.ts')]);
	expect(result.status, result.stderr).toBe(0);
	return jsonFile(join(root, 'actionlint-provider.json'));
}

async function installPackage(root: string): Promise<string> {
	const tarball = join(root, 'provider.tgz');
	const packed = spawnSync(process.execPath, ['pm', 'pack', '--filename', tarball], { cwd: root, encoding: 'utf8' });
	expect(packed.status, packed.stderr).toBe(0);
	const consumer = join(root, 'consumer');
	await mkdir(consumer);
	await writeFile(join(consumer, 'package.json'), '{"name":"consumer","private":true,"type":"module"}');
	const installed = spawnSync(process.execPath, ['install', '--no-save', '--ignore-scripts', tarball], {
		cwd: consumer,
		encoding: 'utf8',
	});
	expect(installed.status, installed.stderr).toBe(0);
	return consumer;
}

async function executeEmbedded(root: string, value: unknown, cwd: string, request: unknown): Promise<unknown> {
	const processSpec = object(object(value).process);
	const program = object(processSpec.program);
	const programPath = join(root, text(program.filename));
	await writeFile(programPath, text(program.inline));
	const args = strings(processSpec.args).map((arg) => arg === '{program-file}' ? programPath : arg);
	const env: Record<string, string> = {};
	for (const name of strings(processSpec.env_passthrough ?? [])) {
		const value = process.env[name];
		if (value !== undefined) env[name] = value;
	}
	const entry = text(processSpec.entry);
	const resolved = spawnSync(entry, ['-p', 'process.execPath'], { encoding: 'utf8' });
	expect(resolved.status, resolved.stderr).toBe(0);
	const result = spawnSync(resolved.stdout.trim(), args, {
		cwd,
		env,
		input: JSON.stringify(request),
		encoding: 'utf8',
	});
	expect(result.status, result.stderr).toBe(0);
	return parseJSON(result.stdout);
}

async function jsonFile(path: string): Promise<unknown> {
	return parseJSON(await readFile(path, 'utf8'));
}
function parseJSON(source: string): unknown {
	return JSON.parse(source);
}
function object(value: unknown): Record<string, unknown> {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('expected object');
	return Object.fromEntries(Object.entries(value));
}
function text(value: unknown): string {
	if (typeof value !== 'string') throw new Error('expected string');
	return value;
}
function strings(value: unknown): string[] {
	if (!Array.isArray(value)) throw new Error('expected array');
	const items: unknown[] = value;
	return items.map(text);
}
