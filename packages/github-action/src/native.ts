import { spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, cp, mkdir, mkdtemp, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, delimiter, dirname, extname, isAbsolute, join, resolve } from 'node:path';

import { normalizeEnvironment } from '#environment';

export function capture(
	executable: string,
	args: string[],
	env: NodeJS.ProcessEnv = process.env,
	options: { timeoutMS?: number; windowsVerbatimArguments?: boolean } = {},
): Promise<{ exitCode: number; stdout: string; stderr: string }> {
	return new Promise((resolve, reject) => {
		const child = spawn(executable, args, {
			env: normalizeEnvironment(env),
			shell: false,
			windowsHide: true,
			stdio: ['ignore', 'pipe', 'pipe'],
			timeout: options.timeoutMS,
			windowsVerbatimArguments: options.windowsVerbatimArguments,
			killSignal: 'SIGKILL',
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

export async function which(
	name: string,
	environment: NodeJS.ProcessEnv = process.env,
	launchers: 'all' | 'native' = 'all',
): Promise<string> {
	const normalized = normalizeEnvironment(environment);
	let extensions = [''];
	if (process.platform === 'win32') {
		if (launchers === 'native' && /\.(?:cmd|bat)$/i.test(name)) return '';
		const supported = launchers === 'native' ? ['.com', '.exe'] : ['.com', '.exe', '.bat', '.cmd'];
		if (!supported.includes(extname(name).toLowerCase())) {
			extensions = [
				...new Set(
					(normalized.PATHEXT || '.COM;.EXE;.BAT;.CMD').split(';')
						.map((extension) => extension.trim().toLowerCase()),
				),
			].filter((extension) => supported.includes(extension));
		}
	}
	const explicitPath = isAbsolute(name) || name.includes('/') || (process.platform === 'win32' && name.includes('\\'));
	const directories = explicitPath
		? ['']
		: (normalized.PATH ?? '').split(delimiter).filter(Boolean);
	for (const directory of directories) {
		for (const extension of extensions) {
			const path = resolve(directory.replace(/^"(.*)"$/, '$1'), name + extension);
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
	if (await completedTool(path)) return path;
	let entries: string[];
	try {
		entries = await readdir(dirname(path));
	} catch (error) {
		if (error instanceof Error && 'code' in error && error.code === 'ENOENT') return '';
		throw error;
	}
	for (const entry of entries.sort()) {
		if (!entry.startsWith(`${basename(path)}-`) || entry.endsWith('.complete')) continue;
		const candidate = join(dirname(path), entry);
		if (await completedTool(candidate)) return candidate;
	}
	return '';
}

async function completedTool(path: string): Promise<boolean> {
	try {
		await access(`${path}.complete`);
		return (await stat(path)).isDirectory();
	} catch (error) {
		if (error instanceof Error && 'code' in error && error.code === 'ENOENT') return false;
		throw error;
	}
}

export async function cacheTool(source: string, name: string, version: string, arch: string): Promise<string> {
	const existing = await findTool(name, version, arch);
	if (existing) return existing;
	const path = cachePath(name, version, arch);
	await mkdir(dirname(path), { recursive: true });
	const installation = await mkdtemp(`${path}-`);
	try {
		await cp(source, installation, { recursive: true });
		// Publish an immutable generation: other installers may still be copying or using theirs.
		await writeFile(`${installation}.complete`, '');
		return installation;
	} catch (error) {
		await rm(installation, { recursive: true, force: true });
		await rm(`${installation}.complete`, { force: true });
		throw error;
	}
}

export async function extractArchive(archive: string, output: string, format: 'zip' | 'tar.gz'): Promise<string> {
	await mkdir(output, { recursive: true });
	if (format === 'tar.gz') await checked('tar', ['-xzf', archive, '-C', output]);
	else if (process.platform === 'win32') {
		const powershell = await which('pwsh', process.env, 'native') || 'powershell.exe';
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
