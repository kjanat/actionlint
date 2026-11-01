import { spawnSync } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

// Authoring fixtures deliberately exercise the current host's real jq/schema
// boundary. Build that host once, then test SDK TypeScript sources directly.
const sdk = resolve(import.meta.dirname, '..');
const repository = resolve(sdk, '../..');
const temporary = await mkdtemp(join(tmpdir(), 'actionlint-sdk-tests-'));
try {
	const executable = join(temporary, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');
	const host = process.env.ACTIONLINT ?? executable;
	if (process.env.ACTIONLINT === undefined) {
		const build = spawnSync('go', ['build', '-p=1', '-o', host, './cmd/actionlint'], {
			cwd: repository,
			env: { ...process.env, GOMAXPROCS: process.env.GOMAXPROCS ?? '2' },
			stdio: 'inherit',
		});
		if (build.error) throw build.error;
		if (build.status !== 0) throw new Error('could not build the host used by SDK fixture tests');
	}
	const requested = process.argv.slice(2);
	const tests = spawnSync(process.execPath, ['test', ...(requested.length === 0 ? ['tests'] : requested)], {
		cwd: sdk,
		env: { ...process.env, ACTIONLINT: host },
		stdio: 'inherit',
	});
	if (tests.error) throw tests.error;
	process.exitCode = tests.status ?? 1;
} finally {
	await rm(temporary, { recursive: true, force: true });
}
