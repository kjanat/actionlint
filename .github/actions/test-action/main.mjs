import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const directory = process.env['INPUT_ACTION-DIRECTORY'];
const binary = process.env.INPUT_BINARY;
if (!directory || !binary) throw new Error('action-directory and binary are required');
const script = fileURLToPath(new URL('../../../tools/action/test-action.mjs', import.meta.url));
const result = spawnSync(process.execPath, [script, directory, binary], {
	env: { ...process.env, ACTIONLINT_TEST_GITHUB_CACHE: '1' },
	stdio: 'inherit',
});
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;
