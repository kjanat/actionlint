import { spawnSync } from 'node:child_process';
import process from 'node:process';

import { resolveBinary } from '#resolve';

const { argv, exit, stderr } = process;

/**
 * @param {string} name - Base name of the executable, without any extension.
 * @returns {void}
 */
export default function launch(name) {
	try {
		const result = spawnSync(resolveBinary(name), argv.slice(2), {
			stdio: 'inherit',
			windowsHide: false,
		});
		if (result.error) throw result.error;
		// On POSIX, re-raise the child's signal. Shells and traps then see the same termination.
		if (result.signal && process.platform !== 'win32') {
			process.removeAllListeners(result.signal);
			process.kill(process.pid, result.signal);
			return;
		}
		exit(result.status ?? 1);
	} catch (err) {
		stderr.write(`${name}: ${err instanceof Error ? err.message : String(err)}\n`);
		exit(1);
	}
}
