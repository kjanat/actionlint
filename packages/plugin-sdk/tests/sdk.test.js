import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, writeFile, mkdir, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { executeScriptCheck, unicodeColumn } from '../dist/index.js';

const sdkRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const cli = join(sdkRoot, 'bin', 'actionlint-plugin.js');

test('rejects malformed JSON requests without calling the check', async () => {
	let called = false;
	const result = await executeScriptCheck(() => {
		called = true;
		return [];
	}, '{}');
	assert.equal(called, false);
	assert.equal(result.ok, false);
	assert.match(result.errors[0], /source, filename/);
});

test('reports callback exceptions in the response envelope', async () => {
	const result = await executeScriptCheck(() => {
		throw new Error('check failed');
	}, JSON.stringify({ source: 'x', filename: 'x.sh' }));
	assert.deepEqual(result, { ok: false, diagnostics: [], errors: ['check failed'] });
});

test('rejects malformed diagnostic fix ranges', async () => {
	const result = await executeScriptCheck(
		({ filename }) => [{
			filename,
			code: 'BAD_FIX',
			message: 'invalid range',
			location: { row: 1, column: 3 },
			fixes: [{ replacements: [{ row: 2, column: 1, end_row: 1, end_column: 1, text: '' }] }],
		}],
		JSON.stringify({ source: 'x', filename: 'x' }),
	);
	assert.deepEqual(result, { ok: false, diagnostics: [], errors: ['checker returned invalid diagnostics'] });
});

test('bounds requests and response serialization', async () => {
	const request = JSON.stringify({ source: 'long', filename: 'x' });
	assert.match((await executeScriptCheck(() => [], request, 100, 3)).errors[0], /request exceeds 3 byte limit/);
	const result = await executeScriptCheck(
		({ filename }) => [{ filename, code: 'X', message: 'long diagnostic', location: { row: 1, column: 1 } }],
		JSON.stringify({ source: 'x', filename: 'x' }),
		10,
	);
	assert.match(result.errors[0], /response exceeds 10 byte limit/);
});

test('counts columns in Unicode scalar values', () => {
	assert.equal(unicodeColumn('é🙂z', 4), 3);
});

test('pack embeds metadata and jq source in the provider root', async () => {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-plugin-sdk-'));
	try {
		await linkSDK(root);
		await writeFile(
			join(root, 'package.json'),
			JSON.stringify({ name: 'actionlint-example-process', version: '1.2.3', type: 'module' }),
		);
		await writeFile(join(root, 'transform.jq'), '.');
		await writeFile(
			join(root, 'actionlint.plugin.ts'),
			`
import { defineProcessProvider, jqOutput } from '@kjanat/actionlint-plugin-sdk';
export default defineProcessProvider({id:'test/example',version:'1.2.3',name:'Example',description:'Sample',license:'MIT',repository:'https://example.test/repo',shells:['bash'],rule:{name:'sample',category:'quality',subcategory:'style',maturity:'stable',description:'Sample rule',provider:'test/example'},entry:'tool',stdin:'request',config:{render:'none'},output:{format:'json',transform:jqOutput('./transform.jq')}});
`,
		);
		const packed = spawnSync(process.execPath, [cli, 'pack', join(root, 'actionlint.plugin.ts')], { encoding: 'utf8' });
		assert.equal(packed.status, 0, packed.stderr);
		const manifest = JSON.parse(await readFile(join(root, 'actionlint-provider.json'), 'utf8'));
		assert.equal(manifest.schema_version, 1);
		assert.equal(manifest.id, manifest.metadata.id);
		assert.equal(manifest.version, manifest.metadata.plugin_version);
		assert.equal(manifest.metadata.metadata_version, 1);
		assert.equal(manifest.metadata.transport, 'process');
		assert.deepEqual(manifest.process.output.transform, { language: 'jq', inline: '.' });
		await assert.rejects(readFile(join(root, 'plugin.metadata.json')));
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});

test('npm pack, install, and execute bundled TypeScript provider', async () => {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-plugin-install-'));
	const packageRoot = join(root, 'author-package');
	const installRoot = join(root, 'installed');
	try {
		await mkdir(packageRoot);
		await linkSDK(packageRoot);
		await writeFile(
			join(packageRoot, 'package.json'),
			JSON.stringify({
				name: 'actionlint-test-semantic-check',
				version: '0.1.0',
				type: 'module',
				exports: { '.': { types: './index.d.ts', import: './index.js' }, './existing': './existing.js' },
				files: ['README.md'],
			}),
		);
		await writeFile(join(packageRoot, 'README.md'), 'plugin test package\n');
		await writeFile(
			join(packageRoot, 'actionlint.plugin.ts'),
			`
import { defineScriptProvider } from '@kjanat/actionlint-plugin-sdk';
export default defineScriptProvider({id:'test/semantic-check',version:'0.1.0',name:'Check',description:'A check',license:'MIT',repository:'https://example.test/repo',shells:['bash'],rule:{name:'check',category:'correctness',subcategory:'shell',maturity:'nursery',description:'Check',provider:'test/semantic-check'},fixes:true,check:({source,filename,config})=>{
 const line=source.split('\\n')[0]; const trimmed=line.replace(/[ \\t]+$/u,''); if(trimmed===line || config?.enabled===false) return [];
 const column=[...trimmed].length+1; const endColumn=[...line].length+1;
 return [{filename,code:'TRAILING_SPACE',message:'trailing whitespace',severity:'warning',location:{row:1,column},end_location:{row:1,column:endColumn},fixes:[{description:'trim',replacements:[{row:1,column,end_row:1,end_column:endColumn,text:''}]}]}];
}});
`,
		);
		const packed = spawnSync(process.execPath, [cli, 'pack', join(packageRoot, 'actionlint.plugin.ts')], {
			encoding: 'utf8',
		});
		assert.equal(packed.status, 0, packed.stderr);
		const manifest = JSON.parse(await readFile(join(packageRoot, 'actionlint-provider.json'), 'utf8'));
		const authorPackage = JSON.parse(await readFile(join(packageRoot, 'package.json'), 'utf8'));
		assert.deepEqual(manifest.selectors.shells, ['bash']);
		assert.equal(manifest.process.entry, 'node');
		assert.deepEqual(manifest.process.args.slice(0, 2), ['--input-type=module', '--eval']);
		assert.equal(manifest.process.args.length, 3);
		assert.match(manifest.process.args[2], /^await import\("actionlint-test-semantic-check\/actionlint"\)$/);
		assert.deepEqual(manifest.process.config, { render: 'json-stdin' });
		assert.deepEqual(manifest.process.output.coordinates, { rows: 'lf', columns: 'chars', end: 'exclusive' });
		assert.deepEqual(authorPackage.exports['.'], { types: './index.d.ts', import: './index.js' });
		assert.equal(authorPackage.exports['./existing'], './existing.js');
		assert.equal(authorPackage.exports['./actionlint'], './actionlint.js');
		assert.deepEqual(authorPackage.files, ['README.md', 'actionlint.js', 'actionlint-provider.json']);
		assert.equal(authorPackage.bin['actionlint-plugin-actionlint-test-semantic-check'], './actionlint.js');
		const npmPacked = spawnSync('npm', ['pack', packageRoot, '--pack-destination', root], { encoding: 'utf8' });
		assert.equal(npmPacked.status, 0, npmPacked.stderr);
		const tarball = join(root, 'actionlint-test-semantic-check-0.1.0.tgz');
		const installed = spawnSync('npm', [
			'install',
			'--prefix',
			installRoot,
			'--offline',
			'--no-audit',
			'--no-fund',
			tarball,
		], { encoding: 'utf8' });
		assert.equal(installed.status, 0, installed.stderr);
		const response = spawnSync(manifest.process.entry, manifest.process.args, {
			cwd: installRoot,
			input: JSON.stringify({ source: 'echo 🙂  \n', filename: 'script.sh', config: { enabled: true } }),
			encoding: 'utf8',
		});
		assert.equal(response.status, 0, response.stderr);
		const output = JSON.parse(response.stdout);
		assert.equal(output.ok, true);
		assert.equal(output.diagnostics[0].location.column, 7);
		assert.equal(output.diagnostics[0].end_location.column, 9);
		assert.deepEqual(output.diagnostics[0].fixes[0].replacements[0], {
			row: 1,
			column: 7,
			end_row: 1,
			end_column: 9,
			text: '',
		});
		const skipped = spawnSync(manifest.process.entry, manifest.process.args, {
			cwd: installRoot,
			input: JSON.stringify({ source: 'echo 🙂  \n', filename: 'script.sh', config: { enabled: false } }),
			encoding: 'utf8',
		});
		assert.equal(JSON.parse(skipped.stdout).diagnostics.length, 0);
		const installedBundle = await readFile(
			join(installRoot, 'node_modules', 'actionlint-test-semantic-check', 'actionlint.js'),
			'utf8',
		);
		assert.match(installedBundle, /^#!\/usr\/bin\/env node/);
		assert.doesNotMatch(installedBundle, /from\s+["'][^"']*actionlint\.plugin\.ts/);
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});

test('init creates a starter provider without overwriting files', async () => {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-plugin-sdk-'));
	try {
		const initialized = spawnSync(process.execPath, [cli, 'init', root], { encoding: 'utf8' });
		assert.equal(initialized.status, 0, initialized.stderr);
		assert.match(await readFile(join(root, 'actionlint.plugin.ts'), 'utf8'), /maturity: 'nursery'/);
		const again = spawnSync(process.execPath, [cli, 'init', root], { encoding: 'utf8' });
		assert.notEqual(again.status, 0);
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});

async function linkSDK(root) {
	const scope = join(root, 'node_modules', '@kjanat');
	await mkdir(scope, { recursive: true });
	await symlink(sdkRoot, join(scope, 'actionlint-plugin-sdk'), process.platform === 'win32' ? 'junction' : 'dir');
}
