import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

function extractionScript() {
	const workflow = readFileSync(new URL('../../.github/workflows/docker-previews.yml', import.meta.url), 'utf8');
	const lines = workflow.split(/\r?\n/);
	const step = lines.findIndex(line => line.trim() === '- name: Extract checksum-verified Docker binaries');
	assert.ok(step >= 0);
	const start = lines.findIndex((line, index) => index > step && line.trim() === 'run: |');
	assert.ok(start > step);
	const shell = lines.slice(step, start).find(line => line.trim().startsWith('shell:'));
	assert.equal(shell?.trim(), 'shell: bash -euo pipefail {0}');
	const script = [];
	for (const line of lines.slice(start + 1)) {
		if (!line.startsWith('          ')) break;
		script.push(line.slice(10));
	}
	assert.ok(script.length > 0);
	return script.join('\n');
}

/** @param {import('node:test').TestContext} t */
function snapshotFixture(t) {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-preview-'));
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const dist = join(root, 'dist');
	const source = join(root, 'source');
	mkdirSync(dist);
	mkdirSync(source);
	const version = '2.0.0-SNAPSHOT-fixture';
	writeFileSync(join(dist, 'metadata.json'), JSON.stringify({ version }));
	/** @type {Map<string, { name: string; checksum: string }>} */
	const archives = new Map();
	for (const arch of ['amd64', 'arm64']) {
		const name = `actionlint_${version}_linux_${arch}.tar.gz`;
		writeFileSync(join(source, 'actionlint'), `binary for ${arch}\n`);
		execFileSync('tar', ['-czf', join(dist, name), '-C', source, 'actionlint']);
		const digest = createHash('sha256').update(readFileSync(join(dist, name))).digest('hex');
		archives.set(arch, { name, checksum: `${digest}  ${name}\n` });
	}
	const checksums = join(dist, `actionlint_${version}_checksums.txt`);
	const complete = [...archives.values()].map(archive => archive.checksum).join('');
	writeFileSync(checksums, complete);
	writeFileSync(join(root, 'extract.bash'), extractionScript());
	const extract = () => {
		const result = spawnSync('bash', ['--noprofile', '--norc', '-euo', 'pipefail', './extract.bash'], {
			cwd: root,
			encoding: 'utf8',
			timeout: 10_000,
		});
		assert.equal(result.error, undefined);
		return result;
	};
	return { root, dist, checksums, complete, archives, extract };
}

test('workflow extracts both Docker binaries after checking their exact archive digests', t => {
	const fixture = snapshotFixture(t);
	// SBOMs and other snapshot artifacts need not be downloaded for this consumer.
	writeFileSync(fixture.checksums, `${fixture.complete}${'0'.repeat(64)}  absent.sbom.json\n`);
	const result = fixture.extract();
	assert.equal(result.status, 0, result.stderr);
	for (const arch of ['amd64', 'arm64']) {
		assert.equal(
			readFileSync(join(fixture.root, '.preview/docker', arch, 'actionlint'), 'utf8'),
			`binary for ${arch}\n`,
		);
	}
});

test('workflow rejects an archive omitted from an otherwise valid checksum manifest', t => {
	const fixture = snapshotFixture(t);
	const amd64 = fixture.archives.get('amd64');
	assert.ok(amd64);
	writeFileSync(fixture.checksums, amd64.checksum);
	const result = fixture.extract();
	assert.notEqual(result.status, 0);
	assert.match(result.stderr, /Expected exactly one checksum for actionlint_.*_linux_arm64\.tar\.gz/);
});

test('workflow rejects a duplicate checksum entry even when both digests match', t => {
	const fixture = snapshotFixture(t);
	const amd64 = fixture.archives.get('amd64');
	assert.ok(amd64);
	writeFileSync(fixture.checksums, fixture.complete + amd64.checksum);
	const result = fixture.extract();
	assert.notEqual(result.status, 0);
	assert.match(result.stderr, /Expected exactly one checksum for actionlint_.*_linux_amd64\.tar\.gz/);
});

test('workflow rejects a listed archive whose digest does not match', t => {
	const fixture = snapshotFixture(t);
	const amd64 = fixture.archives.get('amd64');
	assert.ok(amd64);
	writeFileSync(join(fixture.dist, amd64.name), 'corrupt archive');
	const result = fixture.extract();
	assert.notEqual(result.status, 0);
	assert.match(`${result.stdout}${result.stderr}`, /FAILED/);
});

test('prebuilt CLI stage skips the source compiler while source CLI and Action still build it', () => {
	const dockerfile = readFileSync(new URL('./Dockerfile', import.meta.url), 'utf8');
	/** @type {Map<string, Set<string>>} */
	const dependencies = new Map();
	let stage;
	for (const line of dockerfile.split(/\r?\n/)) {
		const from = /^FROM (\S+) AS (\S+)$/i.exec(line);
		if (from) {
			const [, base, name] = from;
			assert.ok(base && name);
			stage = new Set([base]);
			dependencies.set(name, stage);
		}
		const copy = /^COPY --from=(\S+)/i.exec(line);
		if (copy?.[1]) stage?.add(copy[1]);
	}
	/** @param {string} name @returns {Set<string>} */
	const ancestors = name => {
		const parents = dependencies.get(name) ?? new Set();
		return new Set([...parents].flatMap(parent => [parent, ...ancestors(parent)]));
	};
	assert.ok(dependencies.has('cli-prebuilt'));
	assert.ok(!ancestors('cli-prebuilt').has('builder'));
	assert.ok(![...ancestors('cli-prebuilt')].some(stage => stage.startsWith('golang:')));
	for (const name of ['cli', 'action']) assert.ok(ancestors(name).has('builder'), name);
	assert.equal([...dependencies.keys()].at(-1), 'cli', 'Default Docker builds preserve the source-built CLI');
	assert.match(
		dockerfile,
		/COPY --chmod=755 \.preview\/docker\/\$\{TARGETARCH\}\/actionlint \/usr\/local\/bin\/actionlint/,
	);
});
