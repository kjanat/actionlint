import { appendFile, chmod, copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

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

async function publishBinary(directory: string, name: string, source: string): Promise<void> {
	const executable = join(directory, process.platform === 'win32' ? `${name}.exe` : name);
	await copyFile(source, executable);
	await chmod(executable, 0o755);
}

// Published binaries are invocation-specific job artifacts, never a reusable cache.
export async function publishTools(tools: InstalledTools, environment: Environment): Promise<void> {
	environment = normalizeEnvironment(environment);
	// Step-local PATH entries need not survive into later steps. Invoke selected
	// tools at their original locations so sibling resources remain available.
	const existing = [
		{ name: 'shellcheck', tool: tools.shellcheck },
		{ name: 'pyflakes', tool: tools.pyflakes },
	].flatMap(({ name, tool }) =>
		tool?.kind === 'existing'
			? [{ name, executable: tool.executable }]
			: []
	);
	if (
		!tools.actionlint && tools.shellcheck?.kind !== 'standalone' && tools.pyflakes?.kind !== 'python'
		&& !existing.length
	) return;
	const root = environment.RUNNER_TEMP;
	const pathFile = environment.GITHUB_PATH;
	if (!pathFile) throw new Error('Publishing tools requires GITHUB_PATH');
	if (!root) throw new Error('Publishing tools requires RUNNER_TEMP');
	const pyflakes = tools.pyflakes;
	const directory = await mkdtemp(join(root, 'actionlint-bin-'));
	try {
		for (const tool of existing) await writeWrapper(directory, tool.name, tool.executable);
		if (tools.actionlint) {
			await publishBinary(directory, 'actionlint', tools.actionlint);
		}
		if (tools.shellcheck?.kind === 'standalone') {
			await publishBinary(directory, 'shellcheck', tools.shellcheck.executable);
		}
		if (pyflakes?.kind === 'python') {
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
