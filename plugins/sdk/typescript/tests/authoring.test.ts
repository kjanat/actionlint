import { spawnSync } from 'node:child_process';
import { mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { expect, test } from 'bun:test';

import { compileProvider } from '#authoring';
import { createPluginTester, ProviderTestError } from '#testing';

const sdk = pathToFileURL(resolve(import.meta.dirname, '#index')).href;
const cli = resolve(import.meta.dirname, '#cli');
const mapping =
	'map({filename:$vars.filename,code:.rule,message:.message,severity:"warning",location:{row:.line,column:.column}})';

async function fixture(
	transform: string,
	check: (directory: string, definition: URL) => Promise<void>,
	config = '{render:"none"}',
): Promise<void> {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-sdk-author-'));
	try {
		const definition = join(directory, 'actionlint.plugin.ts');
		await writeFile(
			join(directory, 'package.json'),
			'{"name":"@example/plugin-scanner","version":"1.0.0","type":"module"}\n',
		);
		await writeFile(join(directory, 'output.jq'), transform);
		await writeFile(
			definition,
			`\
import {defineProcessProvider,jqOutput} from ${JSON.stringify(sdk)};
export default defineProcessProvider({
 id:'example/scanner',version:'1.0.0',name:'Scanner',description:'Recorded-output fixture',license:'MIT',repository:'https://github.com/example/scanner',
 rule:{name:'scanner',description:'Scanner findings',category:'correctness',subcategory:'shell',maturity:'stable',provider:'example/scanner'},
 shells:['bash'],entry:'this-scanner-is-intentionally-not-installed',args:[],stdin:'request',config:${config},output:{format:'json',transform:jqOutput('./output.jq')}
});`,
		);
		await check(directory, pathToFileURL(definition));
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
}

test('source compilation embeds jq without creating author build outputs', async () => {
	await fixture(mapping, async (directory, definition) => {
		const before = (await readdir(directory)).sort();
		const manifest = await compileProvider(definition);
		expect(manifest).toMatchObject({
			metadata: { id: 'example/scanner', transport: 'process' },
			process: { output: { transform: { language: 'jq', inline: mapping } } },
		});
		expect((await readdir(directory)).sort()).toEqual(before);
	});
});

test('authoring rejects rule categories outside the SDK union', async () => {
	await fixture(mapping, async (_directory, definition) => {
		const path = fileURLToPath(definition);
		const source = await readFile(path, 'utf8');
		await writeFile(path, source.replace("category:'correctness'", "category:'style'"));
		expect(await compileProvider(definition)).rejects.toThrow('manifest must default-export a valid SDK provider');
	});
});

test('callback manifests are reproducible without staging paths and retain legal comments', async () => {
	await fixture(mapping, async (_directory, definition) => {
		await writeFile(
			fileURLToPath(definition),
			`\
/*! @license Example license */
import { defineScriptProvider } from ${JSON.stringify(sdk)};
export default defineScriptProvider({
 id:'example/deterministic',version:'1.0.0',name:'Deterministic',description:'Repeatable bundle',license:'MIT',repository:'https://github.com/example/plugin',
 rule:{name:'deterministic',description:'Findings',category:'correctness',subcategory:'shell',maturity:'stable',provider:'example/deterministic'},
 shells:['bash'],check:()=>[]
});`,
		);
		const first = await compileProvider(definition);
		expect(await compileProvider(definition)).toEqual(first);
		expect(JSON.stringify(first)).not.toContain('actionlint-plugin-compile-');
		expect(JSON.stringify(first)).toContain('@license Example license');
	});
});

test('compilation reloads edited TypeScript dependencies in the author process', async () => {
	await fixture(mapping, async (directory, definition) => {
		const settings = join(directory, 'settings.ts');
		await writeFile(settings, "export const providerName = 'First';\n");
		const path = fileURLToPath(definition);
		const source = await readFile(path, 'utf8');
		await writeFile(
			path,
			`import { providerName } from './settings.ts';\n${source.replace("name:'Scanner'", 'name:providerName')}`,
		);
		expect(await compileProvider(definition)).toMatchObject({ metadata: { name: 'First' } });
		await writeFile(settings, "export const providerName = 'Second';\n");
		expect(await compileProvider(definition)).toMatchObject({ metadata: { name: 'Second' } });
	});
});

test('authors test captured stdout using the real host without installing the scanner', async () => {
	await fixture(mapping, async (_directory, definition) => {
		const plugin = createPluginTester(definition);
		expect((await plugin.validate()).valid).toBe(true);
		const report = await plugin.assert([{
			name: 'recorded finding',
			request: { source: 'echo ok\n', filename: 'script.sh' },
			stdout: '[{"rule":"CHECK001","message":"Review this","line":1,"column":1}]',
			expect: {
				diagnostics: [{
					filename: 'script.sh',
					code: 'CHECK001',
					message: 'Review this',
					severity: 'warning',
					location: { row: 1, column: 1 },
				}],
			},
		}, {
			name: 'clean output',
			request: { source: 'echo ok\n', filename: 'script.sh' },
			stdout: '[]',
			expect: { diagnostics: [] },
		}]);
		expect(report.cases.map((item) => item.passed)).toEqual([true, true]);
	});
});

test('configuration fixtures exercise config shape, variables and host filename values', async () => {
	await fixture(
		'[]',
		async (directory, definition) => {
			await createPluginTester(definition).assert([{
				name: 'configuration mapping',
				request: { source: 'x', filename: 'script.sh', directory, config: { mode: 'strict' } },
				variables: { filename: 'untrusted.sh', filename_abs: 'untrusted.sh', remove_me: 'old' },
				expect_config: { enabled: true },
				expect_vars: { filename: 'script.sh', filename_abs: join(directory, 'script.sh'), mode: 'strict' },
			}]);
		},
		'{render:"json-stdin",transform:{language:"jq",inline:\'{config:{enabled:true},vars:{mode:.mode,remove_me:null}}\'}}',
	);
});

test('wrong expectations fail while expected genuine decoder failures pass', async () => {
	await fixture(mapping, async (_directory, definition) => {
		const plugin = createPluginTester(definition);
		const wrong = [{
			name: 'wrong expected finding',
			request: { source: 'x', filename: 'script.sh' },
			stdout: '[]',
			expect: {
				diagnostics: [{ filename: 'script.sh', code: 'ABSENT', message: 'Absent', location: { row: 1, column: 1 } }],
			},
		}];
		const report = await plugin.test(wrong);
		expect(report.valid).toBe(false);
		expect(report.cases[0]?.passed).toBe(false);
		await expect(plugin.assert(wrong)).rejects.toBeInstanceOf(ProviderTestError);
		await plugin.assert([{
			name: 'invalid captured JSON',
			request: { source: 'x', filename: 'script.sh' },
			stdout: 'not JSON',
			expect_error: 'decode tool output',
		}]);
		await expect(plugin.assert([])).rejects.toThrow('at least one');
	});
});

test('pack rejects broken jq before changing the author package or writing output', async () => {
	await fixture('map({', async (directory, definition) => {
		const before = await readFile(join(directory, 'package.json'), 'utf8');
		const result = spawnSync(process.execPath, [cli, 'pack', fileURLToPath(definition)], { encoding: 'utf8' });
		expect(result.status).toBe(1);
		expect(result.stderr).toContain('jq');
		expect(await readFile(join(directory, 'package.json'), 'utf8')).toBe(before);
		expect((await readdir(directory)).sort()).toEqual(['actionlint.plugin.ts', 'output.jq', 'package.json']);
	});
});
