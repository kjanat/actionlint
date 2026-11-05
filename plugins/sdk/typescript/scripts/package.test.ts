import { spawnSync } from 'node:child_process';
import { access, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { expect, test } from 'bun:test';

const sdk = resolve(import.meta.dirname, '..');

test('workspace install links the CLI before SDK build outputs exist', async () => {
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-sdk-workspace-'));
	try {
		const provider = join(temporary, 'sdk');
		const consumer = join(temporary, 'consumer');
		await mkdir(provider);
		await mkdir(consumer);
		const entry = await cliEntry(sdk);
		await writeFile(
			join(temporary, 'package.json'),
			JSON.stringify({ private: true, workspaces: ['sdk', 'consumer'] }),
		);
		await writeFile(
			join(provider, 'package.json'),
			JSON.stringify({
				name: '@kjanat-actionlint/plugin-sdk',
				version: '0.1.0',
				type: 'module',
				bin: { 'actionlint-plugin': entry },
			}),
		);
		await writeFile(
			join(consumer, 'package.json'),
			JSON.stringify({
				name: 'plugin-author',
				devDependencies: { '@kjanat-actionlint/plugin-sdk': 'workspace:*' },
				scripts: { probe: 'actionlint-plugin --help' },
			}),
		);
		// Only source files exist at install time. Dist is deliberately absent,
		// as in a fresh checkout; this test isolates package-manager bin linking.
		if (!entry.startsWith('./dist/')) {
			await mkdir(join(provider, 'bin'));
			await copyFile(join(sdk, entry), join(provider, entry));
		}
		run(process.execPath, ['install', '--ignore-scripts'], temporary);
		await mkdir(join(provider, 'dist'));
		await writeFile(
			join(provider, 'dist', 'cli.js'),
			`#!/usr/bin/env node
export const pluginCLI = { run: () => console.log('workspace CLI linked') };
if (import.meta.main) await pluginCLI.run();
`,
			{ mode: 0o755 },
		);
		expect(run(process.execPath, ['run', 'probe'], consumer).trim()).toBe('workspace CLI linked');
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
});

test('installed SDK authors and packs host-tested plugins under Node in ESM and CommonJS projects', async () => {
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-sdk-package-'));
	try {
		const archive = join(temporary, 'sdk.tgz');
		run(process.execPath, ['pm', 'pack', '--filename', archive], sdk);
		for (const type of ['module', 'commonjs']) {
			const consumer = join(temporary, type);
			await mkdir(consumer);
			const specifier = `file:${archive.replaceAll('\\', '/')}`;
			const original = {
				name: `@example/plugin-${type}`,
				version: '4.2.0',
				type,
				main: './existing.js',
				bin: './existing.js',
				files: ['existing.js'],
				scripts: { test: 'existing-test' },
				devDependencies: { '@kjanat-actionlint/plugin-sdk': specifier },
			};
			await writeFile(join(consumer, 'package.json'), JSON.stringify(original));
			await writeFile(
				join(consumer, 'existing.js'),
				type === 'module' ? 'export const value = 42;\n' : 'exports.value = 42;\n',
			);
			await writeFile(join(consumer, 'README.md'), '# Author documentation\n');
			run(process.execPath, ['install', '--ignore-scripts'], consumer);
			const installedSDK = join(consumer, 'node_modules', '@kjanat-actionlint', 'plugin-sdk');
			const cli = join(installedSDK, await cliEntry(installedSDK));
			run('node', [cli, 'init', '--json'], consumer);
			expect(JSON.parse(await readFile(join(consumer, 'package.json'), 'utf8'))).toMatchObject({
				...original,
				files: ['existing.js', 'actionlint-provider.json'],
			});
			expect(await readFile(join(consumer, 'README.md'), 'utf8')).toBe('# Author documentation\n');
			const validation: unknown = JSON.parse(run('node', [cli, 'validate', '--json'], consumer));
			expect(validation).toMatchObject({ schema_version: 1, valid: true });
			const fixtures: unknown = JSON.parse(run('node', [cli, 'test', '--json'], consumer));
			expect(fixtures).toMatchObject({
				schema_version: 1,
				valid: true,
				cases: [{ name: 'scanner finding', passed: true }, { name: 'clean scanner output', passed: true }],
			});
			run('node', [cli, 'pack', '--json'], consumer);
			const packed: unknown = JSON.parse(await readFile(join(consumer, 'actionlint-provider.json'), 'utf8'));
			expect(packed).toMatchObject({
				transport: 'process',
				process: {
					output: { format: 'json', transform: { language: 'jq', inline: expect.stringContaining('$vars.filename') } },
				},
			});
			const providerArchive = join(temporary, `plugin-${type}.tgz`);
			run(process.execPath, ['pm', 'pack', '--filename', providerArchive], consumer);
			const contents = run('tar', ['-tzf', providerArchive], temporary);
			expect(contents).toContain('package/actionlint-provider.json');
			expect(contents).toContain('package/existing.js');
			await writeFile(
				join(consumer, 'actionlint.plugin.ts'),
				`import { defineScriptProvider } from '@kjanat-actionlint/plugin-sdk';
export default defineScriptProvider({
 id:'example/callback',version:'1.0.0',name:'Callback',description:'Node callback',license:'MIT',repository:'https://github.com/example/plugin',
 rule:{name:'callback',description:'Callback findings',category:'correctness',subcategory:'shell',maturity:'stable',provider:'example/callback'},
 shells:['bash'],check:({filename})=>[{filename,code:'CALLBACK',message:'Callback works',location:{row:1,column:1}}]
});`,
			);
			run('node', [cli, 'pack', '--json'], consumer);
			const executable = type === 'module' ? 'actionlint.js' : 'actionlint.mjs';
			expect(JSON.parse(await readFile(join(consumer, 'package.json'), 'utf8'))).toMatchObject({
				type,
				bin: { [`plugin-${type}`]: './existing.js', [`actionlint-plugin-${type}`]: `./${executable}` },
			});
			const checked: unknown = JSON.parse(run(
				'node',
				[join(consumer, executable)],
				consumer,
				JSON.stringify({ source: 'echo ok\n', filename: 'script.sh' }),
			));
			expect(checked).toMatchObject({ ok: true, diagnostics: [{ code: 'CALLBACK', message: 'Callback works' }] });
			const firstBundleManifest = await readFile(join(consumer, 'actionlint-provider.json'), 'utf8');
			run('node', [cli, 'pack', '--json'], consumer);
			expect(await readFile(join(consumer, 'actionlint-provider.json'), 'utf8')).toBe(firstBundleManifest);
			run(process.execPath, ['pm', 'pack', '--filename', providerArchive], consumer);
			const callbackContents = run('tar', ['-tzf', providerArchive], temporary);
			expect(callbackContents).toContain(`package/${executable}`);
			const user = join(temporary, `user-${type}`);
			await mkdir(user);
			await writeFile(join(user, 'package.json'), '{"name":"plugin-user","private":true,"type":"module"}\n');
			run(process.execPath, ['install', '--prod', '--ignore-scripts', '--no-save', providerArchive], user);
			await expect(access(join(user, 'node_modules', '@kjanat-actionlint', 'plugin-sdk'))).rejects.toThrow();
			expect(
				run('node', [
					'--input-type=module',
					'--eval',
					`const plugin = await import(${JSON.stringify(original.name)}); console.log(plugin.value);`,
				], user).trim(),
			).toBe('42');
			const installed: unknown = JSON.parse(run(
				'node',
				[join(user, 'node_modules', '@example', `plugin-${type}`, executable)],
				user,
				JSON.stringify({ source: 'echo ok\n', filename: 'script.sh' }),
			));
			expect(installed).toMatchObject({ ok: true, diagnostics: [{ code: 'CALLBACK' }] });
		}
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
}, 120_000);

async function cliEntry(directory: string): Promise<string> {
	const metadata: unknown = JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'));
	if (typeof metadata !== 'object' || metadata === null || !('bin' in metadata)) {
		throw new Error('SDK package must declare its CLI');
	}
	const bin = metadata.bin;
	if (
		typeof bin !== 'object' || bin === null || !('actionlint-plugin' in bin)
		|| typeof bin['actionlint-plugin'] !== 'string'
	) {
		throw new Error('SDK package must declare actionlint-plugin');
	}
	return bin['actionlint-plugin'];
}

function run(command: string, args: string[], cwd: string, input?: string): string {
	const result = spawnSync(command, args, { cwd, input, encoding: 'utf8', windowsHide: true, timeout: 60_000 });
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`${command} ${args.join(' ')} failed (${result.status}):\n${result.stderr}\n${result.stdout}`);
	}
	return result.stdout;
}
