import { spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, chmod, readFile, stat } from 'node:fs/promises';
import { join } from 'node:path';

import type { ReleaseAsset, RunnerPlatform } from '#assets';
import { checksumForAsset, nativeAssetName, shellcheckAsset, shellcheckVersion } from '#assets';
import { download, downloadVerified } from '#download';
import { subprocessEnvironment } from '#environment';
import { cacheTool, capture, extractArchive, findTool, temporary, which } from '#native';
import type { Environment, ShellcheckCommand, ToolRequirements } from '#runtime';
import { InputError } from '#runtime';
import { commandEscape } from '#workflow';

function selectedTool(name: string, origin: string, path: string, version: string): void {
	console.log(`::debug::${commandEscape(`${name}: ${origin}; version ${version}; ${path}`)}`);
}

async function installedVersion(executable: string): Promise<string> {
	try {
		const result = await capture(executable, ['--version'], process.env, { timeoutMS: 1_000 });
		if (result.exitCode !== 0) return `unavailable (--version exited ${result.exitCode})`;
		const output = `${result.stdout}\n${result.stderr}`.trim();
		const version = /^(?:version:\s*)?(v?\d+\.\d+(?:\.\d+)?(?:[-+][^\s]+)?)(?:\s|$)/im.exec(output)?.[1];
		return version ?? `unavailable (${output ? 'unrecognized' : 'empty'} --version output)`;
	} catch (error) {
		// Version discovery is advisory: an unsupported probe must not replace a working PATH tool.
		const reason = error instanceof Error ? error.message : String(error);
		return `unavailable (${reason.slice(0, 256)})`;
	}
}

export async function checkExecutable(path: string): Promise<void> {
	if (!(await stat(path)).isFile()) throw new Error(`Expected an executable file: ${path}`);
	await access(path, constants.X_OK);
}

async function extract(asset: ReleaseAsset, directory: string): Promise<string> {
	// extractZip on Windows requires a .zip filename.
	const archive = await downloadVerified(
		download,
		asset.url,
		join(directory, asset.archive === 'zip' ? 'archive.zip' : 'archive.tar.gz'),
		asset.sha256,
	);
	const output = join(directory, 'extracted');
	return extractArchive(archive, output, asset.archive);
}

export async function nativeBinary(version: string, platform: RunnerPlatform, directory: string): Promise<string> {
	const binary = platform.os === 'windows' ? 'actionlint.exe' : 'actionlint';
	console.log(`Downloading actionlint ${version} (${platform.os}/${platform.arch})`);
	const name = nativeAssetName(version, platform);
	const base = `https://github.com/kjanat/actionlint/releases/download/v${version}`;
	const checksums = await download(
		`${base}/actionlint_${version}_checksums.txt`,
		join(directory, 'checksums.txt'),
	);
	const sha256 = checksumForAsset(await readFile(checksums, 'utf8'), name);
	const extracted = await extract({
		name,
		url: `${base}/${name}`,
		sha256,
		archive: platform.os === 'windows' ? 'zip' : 'tar.gz',
	}, directory);
	const path = join(extracted, binary);
	if (platform.os !== 'windows') await chmod(path, 0o755);
	await checkExecutable(path);
	selectedTool('actionlint', 'downloaded fallback', path, version);
	return path;
}

export async function shellcheckBinary(platform: RunnerPlatform): Promise<ShellcheckCommand> {
	const existing = await which('shellcheck', process.env, 'native');
	if (existing) {
		selectedTool('ShellCheck', 'existing installation', existing, await installedVersion(existing));
		return { kind: 'existing', executable: existing };
	}

	const binary = platform.os === 'windows' ? 'shellcheck.exe' : 'shellcheck';
	const cacheName = `actionlint-shellcheck-${platform.os}`;
	const cached = await findTool(cacheName, shellcheckVersion, platform.arch);
	if (cached) {
		const path = join(cached, binary);
		await checkExecutable(path);
		selectedTool('ShellCheck', 'tool cache', path, shellcheckVersion);
		return { kind: 'standalone', executable: path };
	}
	console.log(`Installing ShellCheck ${shellcheckVersion}`);
	return temporary(async (directory) => {
		const extracted = await extract(shellcheckAsset(platform), directory);
		const root = platform.os === 'windows' ? extracted : join(extracted, `shellcheck-v${shellcheckVersion}`);
		const path = join(root, binary);
		if (platform.os !== 'windows') await chmod(path, 0o755);
		await checkExecutable(path);
		const executable = join(await cacheTool(root, cacheName, shellcheckVersion, platform.arch), binary);
		selectedTool('ShellCheck', 'downloaded fallback', executable, shellcheckVersion);
		return {
			kind: 'standalone',
			executable,
		};
	});
}

export function executeNative(executable: string, args: string[], environment: Environment): Promise<number> {
	return new Promise((resolve, reject) => {
		const child = spawn(executable, args, {
			env: subprocessEnvironment(environment),
			shell: false,
			windowsHide: true,
			stdio: 'inherit',
		});
		child.once('error', reject);
		child.once('close', (code, signal) => {
			if (code === null) reject(new Error(`actionlint terminated by ${signal}`));
			else resolve(code);
		});
	});
}

export async function inspectTools(executable: string, environment: Environment): Promise<ToolRequirements> {
	const result = await capture(executable, ['-github-action-tools'], environment, { timeoutMS: 300_000 });
	if (result.exitCode !== 0) {
		const message = result.stderr.trim() || `actionlint configuration inspection exited with ${result.exitCode}`;
		if (/(?:unknown flag:|flag provided but not defined:)\s*-*github-action-tools\b/.test(message)) {
			throw new InputError(
				"This actionlint release does not support the native Action protocol. Use 'install-only: true' and run actionlint in a later step, or select a release containing the Node Action.",
			);
		}
		throw result.exitCode === 2 ? new InputError(message) : new Error(message);
	}
	const plan: unknown = JSON.parse(result.stdout);
	if (
		typeof plan !== 'object' || plan === null || !('schema_version' in plan) || plan.schema_version !== 1
		|| !('shellcheck' in plan) || typeof plan.shellcheck !== 'boolean'
	) throw new Error('actionlint returned an unsupported tool plan');
	return { shellcheck: plan.shellcheck };
}
