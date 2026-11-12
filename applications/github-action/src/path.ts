import { appendFile, chmod, copyFile, mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';

import { normalizeEnvironment } from '#environment';
import type { Environment } from '#runtime';

async function publishBinary(directory: string, name: string, source: string): Promise<void> {
	const executable = join(directory, process.platform === 'win32' ? `${name}.exe` : name);
	await copyFile(source, executable);
	await chmod(executable, 0o755);
}

// Published binaries are invocation-specific job artifacts, never a reusable cache.
export async function publishActionlint(executable: string, environment: Environment): Promise<void> {
	environment = normalizeEnvironment(environment);
	const root = environment.RUNNER_TEMP;
	const pathFile = environment.GITHUB_PATH;
	if (!pathFile) throw new Error('Publishing actionlint requires GITHUB_PATH');
	if (!root) throw new Error('Publishing actionlint requires RUNNER_TEMP');
	const directory = await mkdtemp(join(root, 'actionlint-bin-'));
	try {
		await publishBinary(directory, 'actionlint', executable);
		await appendFile(pathFile, `${directory}\n`);
	} catch (error) {
		await rm(directory, { recursive: true, force: true });
		throw error;
	}
}
