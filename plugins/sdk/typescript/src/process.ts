import { spawn } from 'node:child_process';

export interface CommandResult {
	stdout: string;
	stderr: string;
	exitCode: number;
}

/**
 * Run a captured-source tool with separate argument tokens and no shell expansion.
 * Inherit the callback's environment, which the host already filters using the manifest.
 * Supply env to replace that environment for this command; no second SDK allowlist is applied.
 * @example
 * ```ts
 * import { runCommand } from '@kjanat-actionlint/plugin-sdk/process';
 * const result = await runCommand('scanner', ['--stdin'], {
 *   stdin: request.source, allowedExitCodes: [0, 1], timeoutMs: 10_000,
 * });
 * const findings: unknown = JSON.parse(result.stdout);
 * ```
 */
export async function runCommand(
	command: string,
	args: string[],
	options: {
		/** Captured UTF-8 source or request written to the tool's stdin; defaults to empty input. */
		stdin?: string;
		/** Working directory for the child; defaults to the callback's working directory. */
		cwd?: string;
		/** Replace the inherited environment completely. Undefined values omit a variable. */
		env?: Readonly<Record<string, string | undefined>>;
		/** Accepted exit codes; defaults to [0]. Other codes reject with the tool's stderr. */
		allowedExitCodes?: number[];
		/** Kill the child after this many milliseconds; defaults to 60,000. */
		timeoutMs?: number;
	} = {},
): Promise<CommandResult> {
	return new Promise((resolve, reject) => {
		const child = spawn(command, args, {
			cwd: options.cwd,
			env: options.env,
			shell: false,
			windowsHide: true,
			stdio: ['pipe', 'pipe', 'pipe'],
			timeout: options.timeoutMs ?? 60_000,
		});
		const stdout: Buffer[] = [];
		const stderr: Buffer[] = [];
		let size = 0;
		let overflow = false;
		let inputError: Error | undefined;
		const collect = (target: Buffer[]) => (chunk: Buffer) => {
			size += chunk.byteLength;
			if (size > 4 * 1024 * 1024) {
				overflow = true;
				child.kill();
				return;
			}
			target.push(chunk);
		};
		child.stdout.on('data', collect(stdout));
		child.stderr.on('data', collect(stderr));
		child.stdin.on('error', (error) => {
			inputError = error;
		});
		child.on('error', reject);
		child.on('close', (exitCode, signal) => {
			if (overflow) {
				reject(new Error(`${command} output exceeded 4 MiB`));
				return;
			}
			if (inputError !== undefined) {
				reject(new Error(`${command} could not read the complete captured source: ${inputError.message}`));
				return;
			}
			if (exitCode === null || signal !== null) {
				reject(new Error(`${command} terminated before producing a result`));
				return;
			}
			const result = {
				stdout: Buffer.concat(stdout).toString('utf8'),
				stderr: Buffer.concat(stderr).toString('utf8'),
				exitCode,
			};
			if (!(options.allowedExitCodes ?? [0]).includes(exitCode)) {
				const details = result.stderr.trim();
				reject(new Error(`${command} exited ${exitCode}${details ? `: ${details}` : ''}`));
				return;
			}
			resolve(result);
		});
		if (options.stdin === undefined) child.stdin.end();
		else child.stdin.end(options.stdin, 'utf8');
	});
}

/** Require a non-null JSON object before decoding named response fields. */
export function record(value: unknown, label: string): Record<string, unknown> {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) {
		throw new Error(`${label} must be an object`);
	}
	return Object.fromEntries(Object.entries(value));
}

/** Require an array and include its field label in any shape error. */
export function array(value: unknown, label: string): unknown[] {
	if (!Array.isArray(value)) throw new Error(`${label} must be an array`);
	return value;
}

/** Require nonempty text from an external tool response. */
export function text(value: unknown, label: string): string {
	if (typeof value !== 'string' || value.length === 0) throw new Error(`${label} must be a nonempty string`);
	return value;
}

/** Require a one-based positive integer from a tool response. */
export function positiveInteger(value: unknown, label: string): number {
	if (!Number.isInteger(value) || typeof value !== 'number' || value < 1) {
		throw new Error(`${label} must be a positive integer`);
	}
	return value;
}
