import { mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, test } from 'bun:test';
import { formatHelp } from '@kjanat/dreamcli';
import { runCommand } from '@kjanat/dreamcli/testkit';

import { initCommand, packCommand } from '#cli';

test('help examples follow the invoked program name', () => {
	expect(formatHelp(initCommand.schema, { binName: 'custom-plugin' }))
		.toContain('custom-plugin init ./plugins/my-check');
	expect(formatHelp(packCommand.schema, { binName: 'custom-plugin' }))
		.toContain('custom-plugin pack ./plugins/my-check/actionlint.plugin.ts');
});

test('init emits standalone JSON and preserves author files on repeated invocation', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-cli-json-'));
	try {
		const options = {
			jsonMode: true,
			stat: async (): Promise<'directory'> => 'directory',
		};
		const created = await runCommand(initCommand, [directory], options);
		expect(created.exitCode).toBe(0);
		expect(JSON.parse(created.stdout.join(''))).toEqual({ directory });
		expect(created.stderr).toEqual([]);
		expect(created.skippedPathChecks).toEqual([]);
		const definition = join(directory, 'actionlint.plugin.ts');
		await writeFile(definition, '// author content\n');
		const repeated = await runCommand(initCommand, [directory], options);
		expect(repeated.exitCode).not.toBe(0);
		expect(await readFile(definition, 'utf8')).toBe('// author content\n');
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('quiet init creates the plugin without success chatter', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-cli-quiet-'));
	try {
		const result = await runCommand(initCommand, [directory], {
			verbosity: 'quiet',
			pathChecks: 'strict',
			stat: async () => 'directory',
		});
		expect(result.exitCode).toBe(0);
		expect(result.stdout).toEqual([]);
		expect(result.stderr).toEqual([]);
		expect(await readFile(join(directory, 'actionlint.plugin.ts'), 'utf8')).toContain('defineProcessProvider');
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('init preserves existing packages, scripts, documentation and SDK specifiers', async () => {
	for (const specifier of ['workspace:*', 'https://pkg.pr.new/example/sdk@abc123', 'file:./sdk.tgz']) {
		const directory = await mkdtemp(join(tmpdir(), 'actionlint-cli-existing-'));
		try {
			const existing = {
				name: '@author/existing-library',
				version: '4.2.0',
				type: 'module',
				license: 'Apache-2.0',
				exports: { '.': './existing.js' },
				files: ['existing.js'],
				scripts: { test: 'existing-test', 'plugin:test': 'custom-plugin-test' },
				devDependencies: { '@kjanat-actionlint/plugin-sdk': specifier, typescript: '^6' },
			};
			await writeFile(join(directory, 'package.json'), JSON.stringify(existing));
			await writeFile(join(directory, 'README.md'), '# Author documentation\n');
			await writeFile(join(directory, '.gitignore'), '# Author ignores\nsecrets/');
			const result = await runCommand(initCommand, [directory], { stat: async () => 'directory' });
			expect(result.exitCode, result.stderr.join('\n')).toBe(0);
			expect(JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'))).toMatchObject({
				...existing,
				files: ['existing.js', 'actionlint-provider.json'],
				scripts: {
					...existing.scripts,
					'plugin:validate': 'actionlint-plugin validate',
					'plugin:pack': 'actionlint-plugin pack',
				},
			});
			expect(await readFile(join(directory, 'README.md'), 'utf8')).toBe('# Author documentation\n');
			expect(await readFile(join(directory, '.gitignore'), 'utf8')).toBe(
				'# Author ignores\nsecrets/\nnode_modules/\nactionlint-provider.json\nactionlint.js\nactionlint.mjs\n*.tgz\n',
			);
		} finally {
			await rm(directory, { recursive: true, force: true });
		}
	}
});

test('init source collisions leave all existing package files unchanged', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-cli-collision-'));
	try {
		const files = new Map([
			['package.json', '{"name":"author","type":"module","scripts":{"test":"custom"}}\n'],
			['.gitignore', 'private/\n'],
			['actionlint.plugin.ts', '// Author definition\n'],
		]);
		for (const [name, contents] of files) await writeFile(join(directory, name), contents);
		const result = await runCommand(initCommand, [directory], { stat: async () => 'directory' });
		expect(result.exitCode).not.toBe(0);
		for (const [name, contents] of files) expect(await readFile(join(directory, name), 'utf8')).toBe(contents);
		expect((await readdir(directory)).sort()).toEqual([...files.keys()].sort());
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('init preserves equivalent package file paths without adding duplicate entries', async () => {
	for (const manifest of ['/actionlint-provider.json', './actionlint-provider.json', 'actionlint-provider.json']) {
		const directory = await mkdtemp(join(tmpdir(), 'actionlint-cli-package-files-'));
		try {
			const files = ['existing.js', manifest];
			await writeFile(join(directory, 'package.json'), JSON.stringify({ name: 'author', files }));
			const result = await runCommand(initCommand, [directory], { stat: async () => 'directory' });
			expect(result.exitCode, result.stderr.join('\n')).toBe(0);
			expect(JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'))).toMatchObject({ files });
		} finally {
			await rm(directory, { recursive: true, force: true });
		}
	}
});

test('init accepts an explicit SDK tarball without replacing the project module type', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-cli-sdk-'));
	try {
		await writeFile(join(directory, 'package.json'), '{"name":"author","type":"commonjs"}\n');
		const result = await runCommand(initCommand, [directory, '--sdk', 'file:./plugin-sdk.tgz'], {
			stat: async () => 'directory',
		});
		expect(result.exitCode, result.stderr.join('\n')).toBe(0);
		expect(JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'))).toMatchObject({
			type: 'commonjs',
			devDependencies: { '@kjanat-actionlint/plugin-sdk': 'file:./plugin-sdk.tgz' },
		});
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});
