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
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const action = dirname(fileURLToPath(import.meta.url));

/** @param {import('node:test').TestContext} t */
function fixture(t) {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-wasm-toolchain-')).replaceAll('\\', '/');
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const bin = `${root}/bin`;
	mkdirSync(bin);
	copyFileSync(join(action, 'setup.bash'), `${root}/setup.bash`);
	const releases = JSON.parse(readFileSync(join(action, 'releases.json'), 'utf8'));
	for (const release of releases) {
		const base = `${release.name}-${release.version}-x86_64-linux`;
		mkdirSync(`${root}/${base}`);
		writeFileSync(
			`${root}/${base}/${release.name}`,
			`#!/usr/bin/env bash\necho '${release.name} ${release.version}'\necho '${release.name}' >> "$EXECUTIONS"\n`,
		);
		chmodSync(`${root}/${base}/${release.name}`, 0o755);
		const packed = spawnSync('tar', ['-czf', `${root}/${base}.tar.gz`, '-C', root, `${base}/${release.name}`], {
			encoding: 'utf8',
		});
		assert.equal(packed.status, 0, packed.stderr);
		release.sha256 = createHash('sha256').update(readFileSync(`${root}/${base}.tar.gz`)).digest('hex');
	}
	writeFileSync(`${root}/releases.json`, JSON.stringify(releases));
	for (
		const [name, body] of Object.entries({
			gh: 'echo "API request forbidden" >&2; exit 42',
			curl: `printf '%s\\n' "$*" >> "$CALLS"
[[ "\${NETWORK_DISABLED:-}" != true ]] || exit 42
while (($#)); do
  case "$1" in
    https:*) source="$FIXTURE/\${1##*/}" ;;
    --output) destination="$2"; shift ;;
  esac
  shift
done
cp "$source" "$destination"`,
		})
	) {
		writeFileSync(`${bin}/${name}`, `#!/usr/bin/env bash\nset -eu\n${body}\n`);
		chmodSync(`${bin}/${name}`, 0o755);
	}
	/** @param {'prepare' | 'install'} phase @param {Record<string, string>} [extra] */
	const run = (phase, extra = {}) =>
		spawnSync('bash', [
			'-c',
			`
if command -v cygpath >/dev/null; then export FIXTURE="$(cygpath -u "$FIXTURE")"; fi
export PATH="$FIXTURE/bin:$PATH" GITHUB_ACTION_PATH="$FIXTURE" RUNNER_TEMP="$FIXTURE"
export GITHUB_OUTPUT="$FIXTURE/output" GITHUB_PATH="$FIXTURE/path" CALLS="$FIXTURE/calls" EXECUTIONS="$FIXTURE/executions"
bash "$FIXTURE/setup.bash" "$1"`,
			'test',
			phase,
		], {
			encoding: 'utf8',
			env: { ...process.env, FIXTURE: root, RUNNER_OS: 'Linux', RUNNER_ARCH: 'X64', ...extra },
		});
	const calls = () => existsSync(`${root}/calls`) ? readFileSync(`${root}/calls`, 'utf8').trim().split('\n') : [];
	const cache = () =>
		`${root}/wasm-toolchain/${createHash('sha256').update(readFileSync(`${root}/releases.json`)).digest('hex')}`;
	return { root, releases, run, calls, cache };
}

test('cold installs both exact tools without API; warm installs without network', t => {
	const f = fixture(t);
	assert.equal(f.run('prepare').status, 0);
	const cold = f.run('install');
	assert.equal(cold.status, 0, cold.stderr);
	assert.match(cold.stdout, /wasm-tools 1\.258\.0/);
	assert.match(cold.stdout, /wit-bindgen 0\.62\.0/);
	assert.equal(f.calls().length, 2);
	const warm = f.run('install', { NETWORK_DISABLED: 'true' });
	assert.equal(warm.status, 0, warm.stderr);
	assert.equal(f.calls().length, 2);
	assert.equal(readFileSync(`${f.root}/executions`, 'utf8'), 'wasm-tools\nwit-bindgen\nwasm-tools\nwit-bindgen\n');
	assert.ok(readFileSync(`${f.root}/path`, 'utf8').includes('wasm-tools.'));
});

test('corrupt cached bytes require a verified replacement', t => {
	const f = fixture(t);
	assert.equal(f.run('install').status, 0);
	writeFileSync(`${f.cache()}/${f.releases[0].sha256}.tar.gz`, 'tampered cache');
	const result = f.run('install');
	assert.equal(result.status, 0, result.stderr);
	assert.equal(f.calls().length, 3);
});

test('download mismatch cannot execute or publish an installation path', t => {
	const f = fixture(t);
	const first = f.releases[0];
	writeFileSync(`${f.root}/${first.name}-${first.version}-x86_64-linux.tar.gz`, 'tampered release');
	assert.notEqual(f.run('install').status, 0);
	assert.equal(existsSync(`${f.root}/executions`), false);
	assert.equal(existsSync(`${f.root}/path`), false);
	assert.equal(existsSync(`${f.cache()}/${first.sha256}.tar.gz.tmp`), false);
});

test('reviewed version or checksum changes select a new cache', t => {
	const f = fixture(t);
	assert.equal(f.run('prepare').status, 0);
	const before = readFileSync(`${f.root}/output`, 'utf8');
	f.releases[0].version = '1.259.0';
	writeFileSync(`${f.root}/releases.json`, JSON.stringify(f.releases));
	writeFileSync(`${f.root}/output`, '');
	assert.equal(f.run('prepare').status, 0);
	assert.notEqual(readFileSync(`${f.root}/output`, 'utf8'), before);
});

test('unsupported platforms and invalid pins fail before downloading', t => {
	const f = fixture(t);
	for (const extra of [{ RUNNER_OS: 'Windows' }, { RUNNER_ARCH: 'ARM64' }]) {
		assert.notEqual(f.run('install', extra).status, 0);
	}
	f.releases[0].version = '../../escape';
	writeFileSync(`${f.root}/releases.json`, JSON.stringify(f.releases));
	assert.notEqual(f.run('install').status, 0);
	assert.deepEqual(f.calls(), []);
});
