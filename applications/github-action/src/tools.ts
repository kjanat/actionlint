import { spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, chmod, readFile, stat } from 'node:fs/promises';
import { join } from 'node:path';

import type { ReleaseAsset, RunnerPlatform } from '#assets';
import { checksumForAsset, nativeAssetName } from '#assets';
import { download, downloadVerified } from '#download';
import { subprocessEnvironment } from '#environment';
import { extractArchive } from '#native';
import type { Environment } from '#runtime';
import { commandEscape } from '#workflow';

function selectedTool(name: string, origin: string, path: string, version: string): void {
	console.log(`::debug::${commandEscape(`${name}: ${origin}; version ${version}; ${path}`)}`);
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
