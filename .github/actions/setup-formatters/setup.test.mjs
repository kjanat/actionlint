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
	const root = mkdtempSync(join(tmpdir(), 'actionlint-formatters-')).replaceAll('\\', '/');
	t.after(() => rmSync(root, { recursive: true, force: true }));
	mkdirSync(`${root}/bin`);
	copyFileSync(join(action, 'setup.bash'), `${root}/setup.bash`);
	const rows = readFileSync(join(action, 'pins.tsv'), 'utf8').trim().split('\n').map(line => {
		const fields = line.split('\t');
		const name = fields[0];
		writeFileSync(`${root}/${name}.download`, `reviewed ${name} archive`);
		fields[5] = createHash('sha256').update(`reviewed ${name} archive`).digest('hex');
		return fields.join('\t');
	});
	writeFileSync(`${root}/pins.tsv`, `${rows.join('\n')}\n`);
	const identity = createHash('sha256').update(readFileSync(`${root}/pins.tsv`)).digest('hex');
	const mocks = {
		gh: 'echo "unexpected GitHub API request" >&2; exit 42',
		mise: 'echo "unexpected Mise resolution" >&2; exit 42',
		curl: `echo "download $*" >> "$CALLS"
[[ "\${NETWORK_DISABLED:-}" != true ]] || exit 42
name=dprint
[[ "$*" != *tombi* ]] || name=tombi
while (($#)); do
  if [[ "$1" == --output ]]; then cp "$FIXTURE/$name.download" "$2"; exit; fi
  shift
done
exit 1`,
		unzip: `echo 'extract dprint' >> "$CALLS"
printf '#!/usr/bin/env bash\\necho "dprint %s"\\n' "\${DPRINT_VERSION:-0.59.0-kjanat}"`,
		tar: `echo 'extract tombi' >> "$CALLS"
printf '#!/usr/bin/env bash\\necho "tombi 1.8.9"\\n'`,
	};
	for (const [name, body] of Object.entries(mocks)) {
		writeFileSync(`${root}/bin/${name}`, `#!/usr/bin/env bash\nset -eu\n${body}\n`);
		chmodSync(`${root}/bin/${name}`, 0o755);
	}
	/** @param {string} phase @param {Record<string, string>} [extra] */
	const run = (phase, extra = {}) =>
		spawnSync('bash', [
			'-c',
			`
if command -v cygpath >/dev/null; then export FIXTURE="$(cygpath -u "$FIXTURE")"; fi
export PATH="$FIXTURE/bin:$PATH" GITHUB_ACTION_PATH="$FIXTURE" RUNNER_TEMP="$FIXTURE"
export CALLS="$FIXTURE/calls" GITHUB_OUTPUT="$FIXTURE/output" GITHUB_PATH="$FIXTURE/path"
bash "$FIXTURE/setup.bash" "$1"`,
			'test',
			phase,
		], {
			encoding: 'utf8',
			env: { ...process.env, FIXTURE: root, RUNNER_OS: 'Linux', RUNNER_ARCH: 'X64', ...extra },
		});
	return {
		root,
		run,
		identity,
		cache: `${root}/formatter-cache/${identity}`,
		bin: `${root}/formatter-bin/${identity}`,
		calls: () => existsSync(`${root}/calls`) ? readFileSync(`${root}/calls`, 'utf8') : '',
	};
}

test('cold download uses pinned asset URLs; warm archives need no network', t => {
	const f = fixture(t);
	assert.equal(f.run('prepare').status, 0);
	assert.match(readFileSync(`${f.root}/output`, 'utf8'), new RegExp(`cache-key=formatters-linux-x64-v1-${f.identity}`));
	assert.equal(f.calls(), '');
	const cold = f.run('install');
	assert.equal(cold.status, 0, cold.stderr);
	assert.match(f.calls(), /https:\/\/github.com\/kjanat\/dprint\/releases\/download\/0.59.0-kjanat\//);
	assert.match(f.calls(), /https:\/\/github.com\/tombi-toml\/tombi\/releases\/download\/v1.8.9\//);
	writeFileSync(`${f.bin}/dprint`, 'tampered executable');
	const warm = f.run('install', { NETWORK_DISABLED: 'true' });
	assert.equal(warm.status, 0, warm.stderr);
	assert.match(readFileSync(`${f.bin}/dprint`, 'utf8'), /dprint 0.59.0-kjanat/);
	assert.equal(f.calls().split('\n').filter(line => line.startsWith('download ')).length, 2);
});

for (const name of ['dprint', 'tombi']) {
	test(`${name}: tampered download fails before binary publication`, t => {
		const f = fixture(t);
		writeFileSync(`${f.root}/${name}.download`, 'tampered');
		assert.notEqual(f.run('install').status, 0);
		assert.equal(existsSync(`${f.bin}/dprint`), false);
		assert.equal(existsSync(`${f.root}/path`), false);
		assert.doesNotMatch(f.calls(), new RegExp(`extract ${name}`));
	});
	test(`${name}: corrupted cache is rejected even with network disabled`, t => {
		const f = fixture(t);
		assert.equal(f.run('install').status, 0);
		const row = readFileSync(`${f.root}/pins.tsv`, 'utf8').split('\n').find(line => line.startsWith(`${name}\t`));
		assert.ok(row);
		writeFileSync(`${f.cache}/${row.split('\t')[4]}`, 'corrupt cache');
		assert.notEqual(f.run('install', { NETWORK_DISABLED: 'true' }).status, 0);
	});
}

test('wrong executable version is rejected', t => {
	const f = fixture(t);
	assert.notEqual(f.run('install', { DPRINT_VERSION: '0.58.0' }).status, 0);
	assert.equal(existsSync(`${f.root}/path`), false);
});

test('missing SHA pin fails closed', t => {
	const f = fixture(t);
	writeFileSync(`${f.root}/pins.tsv`, readFileSync(`${f.root}/pins.tsv`, 'utf8').replace(/[a-f0-9]{64}/, ''));
	assert.notEqual(f.run('install').status, 0);
	assert.equal(f.calls(), '');
});

for (const [RUNNER_OS, RUNNER_ARCH] of [['Windows', 'X64'], ['macOS', 'ARM64'], ['Linux', 'ARM64']]) {
	test(`unsupported ${RUNNER_OS}/${RUNNER_ARCH} fails before downloading`, t => {
		const f = fixture(t);
		assert.notEqual(f.run('prepare', { RUNNER_OS, RUNNER_ARCH }).status, 0);
		assert.equal(f.calls(), '');
	});
}
