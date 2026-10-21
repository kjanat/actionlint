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

/** @param {import('node:test').TestContext} t @param {string} [arch] */
function fixture(t, arch = 'amd64') {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-pandoc-test-')).replaceAll('\\', '/');
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const bin = `${root}/bin`;
	mkdirSync(bin);
	copyFileSync(join(action, 'setup.bash'), `${root}/setup.bash`);
	const bytes = 'reviewed release bytes';
	const digest = createHash('sha256').update(bytes).digest('hex');
	const asset = `pandoc-3.11-1-${arch}.deb`;
	writeFileSync(`${root}/SHA256SUMS`, `${digest}  ${asset}\n`);
	writeFileSync(`${root}/download`, bytes);
	const mocks = {
		gh: 'echo "unexpected GitHub API request" >&2; exit 42',
		curl: `echo "download $*" >> "$CALLS"
[[ "\${NETWORK_DISABLED:-}" != true ]] || exit 42
while (($#)); do
  if [[ "$1" == --output ]]; then cp "$FIXTURE/download" "$2"; exit; fi
  shift
done
exit 1`,
		dpkg: 'if [[ "$1" == --print-architecture ]]; then echo "$ARCH"; else echo "install $*" >> "$CALLS"; fi',
		sudo: 'exec "$@"',
	};
	for (const [name, body] of Object.entries(mocks)) {
		writeFileSync(`${bin}/${name}`, `#!/usr/bin/env bash\nset -eu\n${body}\n`);
		chmodSync(`${bin}/${name}`, 0o755);
	}
	/** @param {'prepare' | 'install'} phase @param {Record<string, string>} [extra] */
	const run = (phase, extra = {}) =>
		spawnSync('bash', [
			'-c',
			`if command -v cygpath >/dev/null; then
export FIXTURE="$(cygpath -u "$FIXTURE")"
fi
export PATH="$FIXTURE/bin:$PATH" GITHUB_ACTION_PATH="$FIXTURE" RUNNER_TEMP="$FIXTURE"
export CALLS="$FIXTURE/calls" GITHUB_OUTPUT="$FIXTURE/output"
bash "$FIXTURE/setup.bash" "$1"`,
			'test',
			phase,
		], { encoding: 'utf8', env: { ...process.env, FIXTURE: root, ARCH: arch, ...extra } });
	const calls = () => existsSync(`${root}/calls`) ? readFileSync(`${root}/calls`, 'utf8') : '';
	const archive = `${root}/pandoc-cache/${digest}/${asset}`;
	return { root, run, calls, archive, asset, digest };
}

for (const arch of ['amd64', 'arm64']) {
	test(`${arch}: cold download needs no API; warm cache needs no network`, t => {
		const f = fixture(t, arch);
		const prepared = f.run('prepare');
		assert.equal(prepared.status, 0, prepared.stderr);
		assert.match(readFileSync(`${f.root}/output`, 'utf8'), new RegExp(`cache-key=pandoc-deb-v1-${arch}-${f.digest}`));
		assert.equal(f.calls(), '');
		const cold = f.run('install');
		assert.equal(cold.status, 0, cold.stderr);
		assert.match(f.calls(), new RegExp(`https://github.com/jgm/pandoc/releases/download/3.11/${f.asset}`));
		const warm = f.run('install', { NETWORK_DISABLED: 'true' });
		assert.equal(warm.status, 0, warm.stderr);
		assert.equal(f.calls().split('\n').filter(line => line.startsWith('download ')).length, 1);
		assert.equal(f.calls().split('\n').filter(line => line.startsWith('install ')).length, 2);
	});
}

test('download digest mismatch prevents installation and cache publication', t => {
	const f = fixture(t);
	writeFileSync(`${f.root}/download`, 'tampered download');
	assert.notEqual(f.run('install').status, 0);
	assert.doesNotMatch(f.calls(), /install /);
	assert.equal(existsSync(f.archive), false);
	assert.equal(existsSync(`${f.archive}.tmp`), false);
});

test('cached bytes are reverified before installation', t => {
	const f = fixture(t);
	mkdirSync(dirname(f.archive), { recursive: true });
	writeFileSync(f.archive, 'tampered cache');
	assert.notEqual(f.run('install', { NETWORK_DISABLED: 'true' }).status, 0);
	assert.equal(f.calls(), '');
});

test('unsupported architecture fails before download or installation', t => {
	const f = fixture(t, 'riscv64');
	const result = f.run('install');
	assert.notEqual(result.status, 0);
	assert.match(result.stderr, /Unsupported Pandoc architecture/);
	assert.equal(f.calls(), '');
});

test('changed reviewed pin selects a different archive cache', t => {
	const f = fixture(t);
	assert.equal(f.run('prepare').status, 0);
	const before = readFileSync(`${f.root}/output`, 'utf8');
	writeFileSync(`${f.root}/SHA256SUMS`, `${'a'.repeat(64)}  ${f.asset}\n`);
	writeFileSync(`${f.root}/output`, '');
	assert.equal(f.run('prepare').status, 0);
	assert.notEqual(readFileSync(`${f.root}/output`, 'utf8'), before);
});
