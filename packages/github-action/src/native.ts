import { spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, cp, mkdir, mkdtemp, rename, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join } from 'node:path';

import { normalizeEnvironment } from '#environment';

export function capture(
	executable: string,
	args: string[],
	env: NodeJS.ProcessEnv = process.env,
): Promise<{ exitCode: number; stdout: string; stderr: string }> {
	return new Promise((resolve, reject) => {
		const child = spawn(executable, args, {
			env: normalizeEnvironment(env),
			shell: false,
			windowsHide: true,
			stdio: ['ignore', 'pipe', 'pipe'],
		});
		let stdout = '';
		let stderr = '';
		child.stdout.setEncoding('utf8').on('data', (chunk: string) => {
			stdout += chunk;
		});
		child.stderr.setEncoding('utf8').on('data', (chunk: string) => {
			stderr += chunk;
		});
		child.once('error', reject);
		child.once('close', (code, signal) => {
			if (code === null) reject(new Error(`${executable} terminated by ${signal}`));
			else resolve({ exitCode: code, stdout, stderr });
		});
	});
}

async function checked(executable: string, args: string[], env?: NodeJS.ProcessEnv): Promise<void> {
	const result = await capture(executable, args, env);
	if (result.exitCode !== 0) throw new Error(`${executable} exited with ${result.exitCode}: ${result.stderr}`);
}

export async function which(name: string, environment: NodeJS.ProcessEnv = process.env): Promise<string> {
	const extensions = process.platform === 'win32' ? ['.exe'] : [''];
	for (const directory of (normalizeEnvironment(environment).PATH ?? '').split(delimiter).filter(Boolean)) {
		for (const extension of extensions) {
			const path = join(directory.replace(/^"(.*)"$/, '$1'), name + extension);
			try {
				if (!(await stat(path)).isFile()) continue;
				await access(path, constants.X_OK);
				return path;
			} catch (error) {
				if (
					!(error instanceof Error) || !('code' in error)
					|| !['ENOENT', 'ENOTDIR', 'EACCES'].includes(String(error.code))
				) throw error;
			}
		}
	}
	return '';
}

export async function temporary<T>(task: (directory: string) => Promise<T>): Promise<T> {
	const directory = await mkdtemp(join(process.env.RUNNER_TEMP || tmpdir(), 'actionlint-tools-'));
	try {
		return await task(directory);
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
}

function cachePath(name: string, version: string, arch: string): string {
	const root = process.env.RUNNER_TOOL_CACHE;
	if (!root) throw new Error('RUNNER_TOOL_CACHE is required to install external tools');
	return join(root, name, version, arch);
}

export async function findTool(name: string, version: string, arch: string): Promise<string> {
	const path = cachePath(name, version, arch);
	try {
		await access(`${path}.complete`);
		return (await stat(path)).isDirectory() ? path : '';
	} catch (error) {
		if (error instanceof Error && 'code' in error && error.code === 'ENOENT') return '';
		throw error;
	}
}

export async function cacheTool(source: string, name: string, version: string, arch: string): Promise<string> {
	const path = cachePath(name, version, arch);
	await mkdir(dirname(path), { recursive: true });
	const staging = await mkdtemp(`${path}-`);
	try {
		await cp(source, staging, { recursive: true });
		try {
			await rename(staging, path);
		} catch (error) {
			if (await findTool(name, version, arch)) return path;
			throw error;
		}
		await writeFile(`${path}.complete`, '');
		return path;
	} finally {
		await rm(staging, { recursive: true, force: true });
	}
}

export async function extractArchive(archive: string, output: string, format: 'zip' | 'tar.gz'): Promise<string> {
	await mkdir(output, { recursive: true });
	if (format === 'tar.gz') await checked('tar', ['-xzf', archive, '-C', output]);
	else if (process.platform === 'win32') {
		const powershell = await which('pwsh') || 'powershell.exe';
		await checked(powershell, [
			'-NoLogo',
			'-NoProfile',
			'-NonInteractive',
			'-Command',
			`$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression.FileSystem
[System.IO.Compression.ZipFile]::ExtractToDirectory($env:ARCHIVE, $env:DESTINATION)`,
		], { ...normalizeEnvironment(process.env), ARCHIVE: archive, DESTINATION: output });
	} else {
		await checked('unzip', ['-q', archive, '-d', output]);
	}
	return output;
}
