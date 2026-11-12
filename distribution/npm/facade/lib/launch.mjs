import { spawnSync } from 'node:child_process';
import process from 'node:process';

import { resolveBinary } from '#resolve';

const { argv, exit, stderr } = process;

/**
 * Run the prebuilt binary for this host, forwarding argv, stdio and exit status.
 *
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
		// On POSIX, re-raise the child's signal so shells and traps see the same termination.
		// Windows uses the exit status below.
		if (result.signal && process.platform !== 'win32') {
			process.removeAllListeners(result.signal);
			process.kill(process.pid, result.signal);
			return;
		}
		// actionlint exits 1 when it finds problems and 2/3 on bad usage or a
		// fatal error, so the status has to be forwarded verbatim.
		exit(result.status ?? 1);
	} catch (err) {
		stderr.write(`${name}: ${err instanceof Error ? err.message : String(err)}\n`);
		exit(1);
	}
}
