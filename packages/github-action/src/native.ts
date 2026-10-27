import { spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, mkdir, mkdtemp, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { delimiter, extname, isAbsolute, join, resolve } from 'node:path';

import { normalizeEnvironment, subprocessEnvironment } from '#environment';

export function capture(
	executable: string,
	args: string[],
	env: NodeJS.ProcessEnv = process.env,
	options: { timeoutMS?: number; windowsVerbatimArguments?: boolean } = {},
): Promise<{ exitCode: number; stdout: string; stderr: string }> {
	return new Promise((resolve, reject) => {
		const child = spawn(executable, args, {
			env: subprocessEnvironment(env),
			shell: false,
			windowsHide: true,
			stdio: ['ignore', 'pipe', 'pipe'],
			windowsVerbatimArguments: options.windowsVerbatimArguments,
		});
		const deadline = options.timeoutMS
			? setTimeout(() => {
				// Descendants may hold inherited pipes open after the child exits.
				reject(new Error(`${executable} timed out after ${options.timeoutMS}ms`));
				child.kill('SIGKILL');
				child.unref();
				child.stdout.destroy();
				child.stderr.destroy();
			}, options.timeoutMS)
			: undefined;
		let stdout = '';
		let stderr = '';
		child.stdout.setEncoding('utf8').on('data', (chunk: string) => {
			stdout += chunk;
		});
		child.stderr.setEncoding('utf8').on('data', (chunk: string) => {
			stderr += chunk;
		});
		child.once('error', (error) => {
			clearTimeout(deadline);
			reject(error);
		});
		child.once('close', (code, signal) => {
			clearTimeout(deadline);
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
		: normalized.PATH?.split(delimiter) ?? [];
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
		await rm(directory, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
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
