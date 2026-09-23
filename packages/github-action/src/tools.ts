import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { constants } from 'node:fs';
import { access, chmod, readFile, stat, writeFile } from 'node:fs/promises';
import { isAbsolute, join } from 'node:path';

import type { ReleaseAsset, RunnerPlatform } from '#assets';
import {
	checksumForAsset,
	nativeAssetName,
	pyflakesAsset,
	pyflakesVersion,
	shellcheckAsset,
	shellcheckVersion,
} from '#assets';
import { download, downloadVerified } from '#download';
import { normalizeEnvironment, subprocessEnvironment } from '#environment';
import { cacheTool, capture, extractArchive, findTool, temporary, which } from '#native';
import type { Environment, PyflakesCommand, ShellcheckCommand, ToolRequirements } from '#runtime';
import { InputError } from '#runtime';

export async function checkExecutable(path: string): Promise<void> {
	if (!(await stat(path)).isFile()) throw new Error(`Expected an executable file: ${path}`);
	await access(path, constants.X_OK);
}

async function extract(asset: ReleaseAsset, directory: string): Promise<string> {
	// extractZip on Windows requires a .zip filename, including for Python wheels.
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
	return path;
}

export async function shellcheckBinary(platform: RunnerPlatform): Promise<ShellcheckCommand> {
	const existing = await which('shellcheck', process.env, 'native');
	if (existing) return { kind: 'existing', executable: existing };

	const binary = platform.os === 'windows' ? 'shellcheck.exe' : 'shellcheck';
	const cacheName = `actionlint-shellcheck-${platform.os}`;
	const cached = await findTool(cacheName, shellcheckVersion, platform.arch);
	if (cached) {
		const path = join(cached, binary);
		await checkExecutable(path);
		return { kind: 'standalone', executable: path };
	}
	console.log(`Installing ShellCheck ${shellcheckVersion}`);
	return temporary(async (directory) => {
		const extracted = await extract(shellcheckAsset(platform), directory);
		const root = platform.os === 'windows' ? extracted : join(extracted, `shellcheck-v${shellcheckVersion}`);
		const path = join(root, binary);
		if (platform.os !== 'windows') await chmod(path, 0o755);
		await checkExecutable(path);
		return {
			kind: 'standalone',
			executable: join(await cacheTool(root, cacheName, shellcheckVersion, platform.arch), binary),
		};
	});
}

async function pythonBinary(): Promise<string> {
	const probe = 'import sys; print(sys.executable); sys.exit(0 if sys.version_info >= (3, 9) else 1)';
	for (const command of ['python3', 'python', 'py']) {
		const path = await which(command);
		if (!path) continue;
		const args = command === 'py' ? ['-3'] : [];
		args.push('-I', '-c', probe);
		let result: Awaited<ReturnType<typeof capture>>;
		if (process.platform === 'win32' && /\.(?:cmd|bat)$/i.test(path)) {
			// Only this fixed probe crosses cmd.exe. Transport the path and Python code
			// in the child environment; arbitrary lint arguments use sys.executable later.
			const environment = normalizeEnvironment(process.env);
			environment.ACTIONLINT_PYTHON_SHIM = path;
			environment.ACTIONLINT_PYTHON_PROBE = probe;
			const prefix = command === 'py' ? '-3 ' : '';
			result = await capture(
				environment.COMSPEC || 'cmd.exe',
				[
					'/d',
					'/v:off',
					'/s',
					'/c',
					`""%ACTIONLINT_PYTHON_SHIM%" ${prefix}-I -c "import os;exec(os.environ['ACTIONLINT_PYTHON_PROBE'])""`,
				],
				environment,
				{ windowsVerbatimArguments: true },
			);
		} else {
			result = await capture(path, args);
		}
		const executable = result.stdout.trim();
		if (result.exitCode === 0 && isAbsolute(executable)) {
			await checkExecutable(executable);
			return executable;
		}
	}
	throw new Error('pyflakes requires Python 3.9 or newer. Set up Python before this action, or set pyflakes: false.');
}

export async function pyflakesCommand(platform: RunnerPlatform, launcher: string): Promise<PyflakesCommand> {
	const existing = await which('pyflakes', process.env, platform.os === 'windows' ? 'native' : 'all');
	if (existing) {
		return { kind: 'existing', executable: existing };
	}

	const executable = await pythonBinary();
	const digest = createHash('sha256').update(launcher).digest('hex');
	const cacheName = `actionlint-pyflakes-${digest}`;
	const cached = await findTool(cacheName, pyflakesVersion, 'any');
	if (cached) {
		const script = join(cached, 'actionlint-pyflakes.py');
		await access(script, constants.R_OK);
		return { kind: 'python', executable, script };
	}
	console.log(`Installing pyflakes ${pyflakesVersion}`);
	return temporary(async (directory) => {
		const extracted = await extract(pyflakesAsset, directory);
		await writeFile(join(extracted, 'actionlint-pyflakes.py'), launcher);
		const root = await cacheTool(extracted, cacheName, pyflakesVersion, 'any');
		return { kind: 'python', executable, script: join(root, 'actionlint-pyflakes.py') };
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
		throw result.exitCode === 2 ? new InputError(message) : new Error(message);
	}
	const plan: unknown = JSON.parse(result.stdout);
	if (
		typeof plan !== 'object' || plan === null || !('schema_version' in plan) || plan.schema_version !== 1
		|| !('shellcheck' in plan) || typeof plan.shellcheck !== 'boolean'
		|| !('pyflakes' in plan) || typeof plan.pyflakes !== 'boolean'
	) throw new Error('actionlint returned an unsupported tool plan');
	return { shellcheck: plan.shellcheck, pyflakes: plan.pyflakes };
}
