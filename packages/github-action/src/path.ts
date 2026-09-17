import { appendFile, chmod, copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';

import type { Environment, InstalledTools } from '#runtime';

function shellQuote(value: string): string {
	return `'${value.replaceAll("'", "'\\''")}'`;
}

// Published binaries are invocation-specific job artifacts, never a reusable cache.
export async function publishTools(tools: InstalledTools, environment: Environment): Promise<void> {
	if (!tools.actionlint && !tools.shellcheck && !tools.pyflakes) return;
	const root = environment.RUNNER_TEMP;
	const pathFile = environment.GITHUB_PATH;
	if (!pathFile) throw new Error('Publishing tools requires GITHUB_PATH');
	const pyflakes = tools.pyflakes;
	let directory: string | undefined;
	if (tools.actionlint || pyflakes?.kind === 'python') {
		if (!root) throw new Error('Publishing actionlint or the pyflakes wrapper requires RUNNER_TEMP');
		directory = await mkdtemp(join(root, 'actionlint-bin-'));
	}
	try {
		const directories = new Set<string>();
		if (tools.shellcheck) directories.add(dirname(tools.shellcheck));
		if (pyflakes?.kind === 'command') directories.add(dirname(pyflakes.executable));
		if (directory && tools.actionlint) {
			const executable = join(directory, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');
			await copyFile(tools.actionlint, executable);
			await chmod(executable, 0o755);
		}
		if (directory && pyflakes?.kind === 'python') {
			await writeFile(
				join(directory, 'pyflakes'),
				`#!/bin/sh\nexec ${shellQuote(pyflakes.executable)} -I ${shellQuote(pyflakes.script)} "$@"\n`,
				{ mode: 0o755 },
			);
			if (process.platform === 'win32') {
				const python = pyflakes.executable.replaceAll('%', '%%');
				const script = pyflakes.script.replaceAll('%', '%%');
				await writeFile(
					join(directory, 'pyflakes.cmd'),
					`@echo off\r\nsetlocal DisableDelayedExpansion\r\n"${python}" -I "${script}" %*\r\nexit /b %errorlevel%\r\n`,
				);
			}
		}
		// The runner prepends entries; publish our fresh binaries last so they win.
		if (directory) directories.add(directory);
		await appendFile(pathFile, `${[...directories].join('\n')}\n`);
	} catch (error) {
		if (directory) await rm(directory, { recursive: true, force: true });
		throw error;
	}
}
