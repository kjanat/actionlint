import { appendFile, chmod, copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { extname, join } from 'node:path';

import { normalizeEnvironment } from '#environment';
import type { Environment, InstalledTools } from '#runtime';

function shellQuote(value: string): string {
	return `'${value.replaceAll("'", "'\\''")}'`;
}

async function writeWrapper(directory: string, name: string, executable: string, args: string[] = []): Promise<void> {
	const command = [executable, ...args];
	await writeFile(
		join(directory, name),
		`#!/bin/sh\nexec ${command.map(shellQuote).join(' ')} "$@"\n`,
		{ mode: 0o755 },
	);
	if (process.platform === 'win32') {
		const windowsCommand = command.map((argument) => `"${argument.replaceAll('%', '%%')}"`).join(' ');
		await writeFile(
			join(directory, `${name}.cmd`),
			`@echo off\r\nsetlocal DisableDelayedExpansion\r\n${windowsCommand} %*\r\nexit /b %errorlevel%\r\n`,
		);
	}
}

async function publishCommand(directory: string, name: string, executable: string): Promise<void> {
	const extension = extname(executable).toLowerCase();
	if (process.platform === 'win32' && ['.exe', '.com'].includes(extension)) {
		// Preserve direct executable spawning for native command consumers.
		await copyFile(executable, join(directory, `${name}${extension}`));
	} else {
		await writeWrapper(directory, name, executable);
	}
}

// Published binaries are invocation-specific job artifacts, never a reusable cache.
export async function publishTools(tools: InstalledTools, environment: Environment): Promise<void> {
	environment = normalizeEnvironment(environment);
	if (!tools.actionlint && !tools.shellcheck && !tools.pyflakes) return;
	const root = environment.RUNNER_TEMP;
	const pathFile = environment.GITHUB_PATH;
	if (!pathFile) throw new Error('Publishing tools requires GITHUB_PATH');
	if (!root) throw new Error('Publishing tools requires RUNNER_TEMP');
	const pyflakes = tools.pyflakes;
	const directory = await mkdtemp(join(root, 'actionlint-bin-'));
	try {
		if (tools.actionlint) {
			const executable = join(directory, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');
			await copyFile(tools.actionlint, executable);
			await chmod(executable, 0o755);
		}
		if (tools.shellcheck) {
			await publishCommand(directory, 'shellcheck', tools.shellcheck);
		}
		if (pyflakes?.kind === 'command') {
			await publishCommand(directory, 'pyflakes', pyflakes.executable);
		} else if (pyflakes?.kind === 'python') {
			await writeWrapper(
				directory,
				'pyflakes',
				pyflakes.executable,
				['-I', pyflakes.script],
			);
		}
		// Export selected tools only; their siblings must not reorder other toolchains.
		await appendFile(pathFile, `${directory}\n`);
	} catch (error) {
		await rm(directory, { recursive: true, force: true });
		throw error;
	}
}
