import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { DocumentRequest } from './index.ts';

/**
 * Materialize a captured document at the conventional path expected by file-based tools.
 * The temporary directory is removed even when the callback fails.
 * @example
 * ```ts
 * import { withCapturedFile } from '@kjanat-actionlint/plugin-sdk/files';
 * const output = await withCapturedFile(request, (directory, file) =>
 *   runCommand('yamllint', [file], { cwd: directory, allowedExitCodes: [0, 1] }));
 * ```
 */
export async function withCapturedFile<T>(
	request: DocumentRequest,
	check: (directory: string, relativeFile: string) => Promise<T>,
): Promise<T> {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-external-linter-'));
	const relativeFile = request.kind === 'workflow' ? '.github/workflows/captured.yml' : 'action.yml';
	const file = join(directory, relativeFile);
	try {
		await mkdir(join(directory, request.kind === 'workflow' ? '.github/workflows' : ''), { recursive: true });
		await writeFile(file, request.source, { flag: 'wx' });
		return await check(directory, relativeFile);
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
}
