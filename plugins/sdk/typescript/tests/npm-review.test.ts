import { spawnSync } from 'node:child_process';
import { access, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { expect, test } from 'bun:test';
import { runCommand } from '@kjanat/dreamcli/testkit';

import { initCommand, packCommand } from '#cli';

function run(command: string, args: string[], cwd: string): string {
	const result = spawnSync(command, args, { cwd, encoding: 'utf8', windowsHide: true, timeout: 60000 });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${command}: ${result.stderr}`);
	return result.stdout;
}

test('npm pack and install retain the provider and existing publication rules', async () => {
	const temporary = await mkdtemp(join(tmpdir(), 'sdk-npm-review-'));
	try {
		for (const variant of ['gitignore', 'npmignore', 'files']) {
			const directory = join(temporary, variant);
			await mkdir(directory);
			const files = variant === 'files' ? ['existing*.js'] : undefined;
			await writeFile(
				join(directory, 'package.json'),
				JSON.stringify({ name: `example-${variant}`, version: '1.0.0', type: 'module', files }),
			);
			await writeFile(join(directory, 'existing.js'), 'export default 42;\n');
			await writeFile(join(directory, 'secret.txt'), 'secret\n');
			await writeFile(join(directory, '.gitignore'), 'secret.txt\n');
			if (variant === 'npmignore') {
				await writeFile(join(directory, '.npmignore'), 'secret.txt\nactionlint-provider.json\n');
			}
			const init = await runCommand(initCommand, [directory], { stat: async () => 'directory' });
			expect(init.exitCode, init.stderr.join('\n')).toBe(0);
			const definition = join(directory, 'actionlint.plugin.ts');
			const source = await readFile(definition, 'utf8');
			const sdk = resolve(import.meta.dirname, '../src/index.ts').replaceAll('\\', '/');
			await writeFile(definition, source.replace('@kjanat-actionlint/plugin-sdk', sdk));
			const packed = await runCommand(packCommand, [definition], { stat: async () => 'file' });
			expect(packed.exitCode, packed.stderr.join('\n')).toBe(0);
			const output = run('npm', ['pack', '--ignore-scripts', '--json'], directory);
			const archive = join(directory, `example-${variant}-1.0.0.tgz`);
			expect(output).toContain('actionlint-provider.json');
			const consumer = join(temporary, `consumer-${variant}`);
			await mkdir(consumer);
			await writeFile(join(consumer, 'package.json'), '{"name":"consumer","version":"1.0.0","private":true}');
			run('npm', ['install', '--ignore-scripts', '--no-audit', '--no-fund', archive], consumer);
			const installed = join(consumer, 'node_modules', `example-${variant}`);
			await access(join(installed, 'actionlint-provider.json'));
			await access(join(installed, 'existing.js'));
			await expect(access(join(installed, 'secret.txt'))).rejects.toThrow();
		}
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
}, 120000);
