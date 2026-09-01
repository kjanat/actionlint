import { resolveBinary } from '#resolve';
import { spawnSync } from 'node:child_process';
import process from 'node:process';

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
		// The child died from a signal (SIGINT, SIGTERM, ...). Re-raise it on
		// ourselves so the parent shell sees WIFSIGNALED / exit code 128 + N
		// rather than a generic 1; `set -e`, trap handlers and Ctrl+C chaining
		// all depend on that. POSIX only: Windows has no signal exit semantics,
		// so fall through to the generic failure exit there.
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
