import assert from 'node:assert/strict';
import test from 'node:test';

import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import {
	chmodSync,
	copyFileSync,
	existsSync,
	mkdirSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const action = dirname(fileURLToPath(import.meta.url)).replaceAll('\\', '/');
const msysTemporaryDirectory = process.platform === 'win32'
	? spawnSync('bash', ['-c', 'cygpath -m /tmp'], { encoding: 'utf8' }).stdout.trim()
	: '';

/** @param {import('node:test').TestContext} t */
function fixture(t) {
	const directory = mkdtempSync(join(tmpdir(), 'actionlint-shellcheck-cache-')).replaceAll('\\', '/');
	t.after(() => rmSync(directory, { recursive: true, force: true }));
	const bin = `${directory}/bin`;
	mkdirSync(bin);
	for (const name of ['setup.bash', 'retry.bash', 'pinned-release.json']) {
		copyFileSync(`${action}/${name}`, `${directory}/${name}`);
	}
	writeFileSync(
		`${bin}/gh`,
		`#!/usr/bin/env bash
set -eu
printf '%s\\n' "$*" >> "$CALLS"
if [[ "\${GH_FAIL:-}" == true ]]; then exit 42; fi
if [[ -n "\${GH_HEADERS:-}" ]]; then printf '%b\\n\\n' "$GH_HEADERS"; exit 1; fi
if [[ "$1" == api ]]; then printf 'HTTP/2.0 200 OK\\n\\n'; cat "$RELEASE"; exit; fi
exit 1
`,
	);
	chmodSync(`${bin}/gh`, 0o755);
	writeFileSync(
		`${bin}/curl`,
		`#!/usr/bin/env bash
set -eu
printf 'curl %s\\n' "$*" >> "$CALLS"
if [[ "\${NETWORK_FAIL:-}" == true ]]; then exit 42; fi
while (($#)); do
  if [[ "$1" == --output ]]; then cp "$ARCHIVE" "$2"; exit; fi
  shift
done
exit 1
`,
	);
	chmodSync(`${bin}/curl`, 0o755);
	// Retry delays are irrelevant to these offline installation scenarios.
	writeFileSync(`${bin}/sleep`, '#!/usr/bin/env bash\nprintf "%s\\n" "$1" >> "$SLEEPS"\n');
	chmodSync(`${bin}/sleep`, 0o755);
	const environment = {
		...process.env,
		GITHUB_ACTION_PATH: directory,
		RUNNER_TEMP: directory,
		RUNNER_OS: 'Linux',
		RUNNER_ARCH: 'X64',
		GH_REPO: 'koalaman/shellcheck',
		UPDATE_PATH: 'true',
		CALLS: `${directory}/calls`,
		RELEASE: `${directory}/release.json`,
		ARCHIVE: `${directory}/release.tar.gz`,
		GITHUB_OUTPUT: `${directory}/output`,
		GITHUB_PATH: `${directory}/path`,
		SLEEPS: `${directory}/sleeps`,
	};
	/** @param {string} tag */
	const release = tag => {
		const extracted = `${directory}/shellcheck-${tag}`;
		mkdirSync(extracted, { recursive: true });
		writeFileSync(`${extracted}/shellcheck`, `#!/usr/bin/env bash\nprintf 'version: ${tag.slice(1)}\\n'\n`);
		chmodSync(`${extracted}/shellcheck`, 0o755);
		const packed = spawnSync(
			'bash',
			['-c', 'cd "$RUNNER_TEMP"; tar -czf release.tar.gz "shellcheck-$1"', 'test', tag],
			{
				env: environment,
				encoding: 'utf8',
			},
		);
		assert.equal(packed.status, 0, packed.stderr);
		const digest = createHash('sha256').update(readFileSync(environment.ARCHIVE)).digest('hex');
		writeFileSync(
			environment.RELEASE,
			JSON.stringify({
				repository: 'koalaman/shellcheck',
				tag_name: tag,
				assets: [{ name: `shellcheck-${tag}.linux.x86_64.tar.gz`, digest: `sha256:${digest}` }],
			}),
		);
		if (tag === 'v0.11.0') copyFileSync(environment.RELEASE, `${directory}/pinned-release.json`);
	};
	/** @param {'prepare' | 'install'} phase @param {Record<string, string>} extra */
	const run = (phase, extra) => {
		writeFileSync(environment.GITHUB_OUTPUT, '');
		return spawnSync('bash', [
			'-c',
			`
if command -v cygpath >/dev/null; then
  export MOCK_BIN="$(cygpath -u "$MOCK_BIN")"
  export RUNNER_TEMP="$(cygpath -u "$RUNNER_TEMP")"
  if [[ -n "\${CACHE_DIR:-}" ]]; then export CACHE_DIR="$(cygpath -u "$CACHE_DIR")"; fi
fi
export PATH="$MOCK_BIN:$PATH"
bash "$GITHUB_ACTION_PATH/setup.bash" "$1"
`,
			'test',
			phase,
		], {
			env: { ...environment, MOCK_BIN: bin, ...extra },
			encoding: 'utf8',
		});
	};
	const outputs = () =>
		Object.fromEntries(
			readFileSync(environment.GITHUB_OUTPUT, 'utf8').trim().split('\n').filter(Boolean).map(line => {
				const separator = line.indexOf('=');
				const value = line.slice(separator + 1);
				const native = process.platform === 'win32'
					? value.replace(/^\/tmp(?=\/|$)/, msysTemporaryDirectory).replace(/^\/([A-Za-z])\//, '$1:/')
					: value;
				return [line.slice(0, separator), native];
			}),
		);
	/** @param {string} version @param {Record<string, string>} [extra] */
	const prepare = (version, extra = {}) => {
		const result = run('prepare', { VERSION: version, ...extra });
		assert.equal(result.status, 0, result.stdout + result.stderr);
		const prepared = outputs();
		return { TAG: prepared.tag, CACHE_DIR: prepared['cache-dir'], RELEASE_FILE: prepared['release-file'] };
	};
	const calls = () => existsSync(environment.CALLS) ? readFileSync(environment.CALLS, 'utf8').trim().split('\n') : [];
	return { environment, release, run, prepare, outputs, calls };
}

test('cold install verifies the release; normalized pinned warm install works without GitHub', t => {
	const f = fixture(t);
	f.release('v0.11.0');
	const cold = f.prepare('0.11.0');
	assert.deepEqual(f.calls(), []);
	const installed = f.run('install', cold);
	assert.equal(installed.status, 0, installed.stdout + installed.stderr);
	assert.match(installed.stdout, /version: 0\.11\.0/);
	assert.equal(f.calls().length, 1);
	assert.equal(f.calls().filter(call => call.startsWith('api ')).length, 0);
	assert.match(
		f.calls()[0],
		/https:\/\/github\.com\/koalaman\/shellcheck\/releases\/download\/v0\.11\.0\/shellcheck-v0\.11\.0\.linux\.x86_64\.tar\.gz/,
	);
	assert.ok(existsSync(f.outputs()['shellcheck-path']));
	const pathBefore = readFileSync(f.environment.GITHUB_PATH, 'utf8');
	assert.ok(pathBefore.includes(basename(dirname(f.outputs()['shellcheck-path']))));
	const warm = f.prepare('v0.11.0', { GH_FAIL: 'true' });
	assert.equal(warm.CACHE_DIR, cold.CACHE_DIR);
	const reused = f.run('install', { ...warm, GH_FAIL: 'true', NETWORK_FAIL: 'true', UPDATE_PATH: 'false' });
	assert.equal(reused.status, 0, reused.stdout + reused.stderr);
	assert.match(reused.stdout, /version: 0\.11\.0/);
	assert.equal(f.calls().length, 1);
	assert.equal(readFileSync(f.environment.GITHUB_PATH, 'utf8'), pathBefore);
});

test('corrupt cached bytes are replaced only by a verified release archive', t => {
	const f = fixture(t);
	f.release('v0.11.0');
	const initial = f.prepare('0.11.0');
	assert.equal(f.run('install', initial).status, 0);
	writeFileSync(`${initial.CACHE_DIR}/archive`, 'corrupt');
	const repaired = f.run('install', f.prepare('0.11.0'));
	assert.equal(repaired.status, 0, repaired.stdout + repaired.stderr);
	assert.match(repaired.stdout, /Discarding corrupt ShellCheck cache/);
	assert.match(repaired.stdout, /version: 0\.11\.0/);
	assert.equal(f.calls().length, 2);
});

test('checksum mismatch fails before caching or executing the download', t => {
	const f = fixture(t);
	f.release('v0.11.0');
	writeFileSync(f.environment.ARCHIVE, 'substituted download');
	const prepared = f.prepare('0.11.0');
	const result = f.run('install', prepared);
	assert.notEqual(result.status, 0);
	assert.match(result.stdout, /does not match its SHA-256 checksum/);
	assert.equal(existsSync(`${prepared.CACHE_DIR}/archive`), false);
	assert.deepEqual(f.outputs(), {});
});

test('latest resolves every invocation and selects a new immutable version cache', t => {
	const f = fixture(t);
	f.release('v0.11.0');
	const previous = f.prepare('latest');
	assert.equal(f.run('install', previous).status, 0);
	f.release('v0.12.0');
	const current = f.prepare('latest');
	assert.notEqual(current.CACHE_DIR, previous.CACHE_DIR);
	const installed = f.run('install', current);
	assert.equal(installed.status, 0, installed.stdout + installed.stderr);
	assert.match(installed.stdout, /version: 0\.12\.0/);
	assert.equal(f.calls().filter(call => call.includes('releases/latest')).length, 2);
	assert.equal(f.calls().filter(call => call.includes('releases/tags')).length, 0);
});

test('platform caches stay separate and invalid inputs fail before network access', t => {
	const f = fixture(t);
	const linux = f.prepare('0.11.0');
	for (
		const [RUNNER_OS, RUNNER_ARCH] of [['Linux', 'ARM64'], ['macOS', 'X64'], ['macOS', 'ARM64'], ['Windows', 'X64'], [
			'Windows',
			'ARM64',
		]]
	) {
		assert.notEqual(f.prepare('0.11.0', { RUNNER_OS, RUNNER_ARCH }).CACHE_DIR, linux.CACHE_DIR);
	}
	/** @type {Record<string, string>[]} */
	const invalid = [{ VERSION: '../oops' }, { UPDATE_PATH: 'maybe' }, { RUNNER_ARCH: 'unknown' }];
	for (const extra of invalid) {
		assert.notEqual(f.run('prepare', { VERSION: '0.11.0', ...extra }).status, 0);
	}
	assert.deepEqual(f.calls(), []);
});

test('a forged cached digest cannot replace the reviewed release pin', t => {
	const f = fixture(t);
	f.release('v0.11.0');
	const prepared = f.prepare('0.11.0', { GH_FAIL: 'true' });
	assert.equal(f.run('install', prepared).status, 0);
	writeFileSync(`${prepared.CACHE_DIR}/archive`, 'forged cache');
	writeFileSync(`${prepared.CACHE_DIR}/sha256`, createHash('sha256').update('forged cache').digest('hex'));
	const repaired = f.run('install', f.prepare('0.11.0', { GH_FAIL: 'true' }));
	assert.equal(repaired.status, 0, repaired.stderr);
	assert.match(repaired.stdout, /Discarding corrupt ShellCheck cache/);
});

test('an unpinned version uses the requested release metadata', t => {
	const f = fixture(t);
	f.release('v0.12.0');
	const result = f.run('install', f.prepare('v0.12.0'));
	assert.equal(result.status, 0, result.stderr);
	assert.match(f.calls()[0], /releases\/tags\/v0\.12\.0/);
});

test('dynamic API retry respects Retry-After instead of short exponential retries', t => {
	const f = fixture(t);
	const result = f.run('prepare', { VERSION: 'latest', GH_HEADERS: 'HTTP/2.0 429 Too Many Requests\nRetry-After: 17' });
	assert.notEqual(result.status, 0);
	assert.equal(f.calls().length, 3);
	assert.equal(readFileSync(f.environment.SLEEPS, 'utf8'), '17\n17\n');
});

test('exhausted primary quota fails without requests before reset', t => {
	const f = fixture(t);
	const reset = Math.floor(Date.now() / 1000) + 3600;
	const result = f.run('prepare', {
		VERSION: 'latest',
		GH_HEADERS: `HTTP/2.0 403 Forbidden\nX-RateLimit-Remaining: 0\nX-RateLimit-Reset: ${reset}`,
	});
	assert.notEqual(result.status, 0);
	assert.equal(f.calls().length, 1);
	assert.equal(existsSync(f.environment.SLEEPS), false);
	assert.match(result.stderr, /retry after the reset/);
});

test('reviewed pins cover every supported platform asset', () => {
	const release = JSON.parse(readFileSync(`${action}/pinned-release.json`, 'utf8'));
	assert.equal(release.repository, 'koalaman/shellcheck');
	assert.equal(release.tag_name, 'v0.11.0');
	assert.deepEqual(release.assets.map(asset => asset.name).sort(), [
		'shellcheck-v0.11.0.darwin.aarch64.tar.gz',
		'shellcheck-v0.11.0.darwin.x86_64.tar.gz',
		'shellcheck-v0.11.0.linux.aarch64.tar.gz',
		'shellcheck-v0.11.0.linux.x86_64.tar.gz',
		'shellcheck-v0.11.0.zip',
	]);
	for (const asset of release.assets) assert.match(asset.digest, /^sha256:[a-f0-9]{64}$/);
});

test('primary reset takes precedence over a shorter Retry-After', t => {
	const f = fixture(t);
	const reset = Math.floor(Date.now() / 1000) + 90;
	const result = f.run('prepare', {
		VERSION: 'latest',
		GH_HEADERS: `HTTP/2.0 403 Forbidden\nRetry-After: 1\nX-RateLimit-Remaining: 0\nX-RateLimit-Reset: ${reset}`,
	});
	assert.notEqual(result.status, 0);
	for (const delay of readFileSync(f.environment.SLEEPS, 'utf8').trim().split('\n').map(Number)) {
		assert.ok(delay >= 80 && delay <= 91, `retry delay ${delay} ignored reset`);
	}
});
