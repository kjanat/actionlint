import assert from 'node:assert/strict';
import test from 'node:test';

import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { updateToolchain } from './update-go-toolchain.mjs';

const oldRevision = 'a'.repeat(40);
const newRevision = 'b'.repeat(40);

/**
 * @param {import('node:test').TestContext} t
 * @param {{available?: boolean, mismatch?: boolean, checkFails?: boolean, currentMatches?: boolean, missingFamily?: boolean, missingAttribute?: boolean}} options
 */
function fixture(
	t,
	{
		available = true,
		mismatch = false,
		checkFails = false,
		currentMatches = false,
		missingFamily = false,
		missingAttribute = false,
	} = {},
) {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-go-update-'));
	t.after(() => rmSync(root, { recursive: true, force: true }));
	/** @type {Record<string, string>} */
	const files = {
		'go.mod': 'module test\n\ngo 1.26.0\n\ntoolchain go1.98.2\n',
		Dockerfile: 'ARG GOLANG_VER=1.98.2\n# Keep other settings\n',
		'CONTRIBUTING.md': 'docker build --build-arg GOLANG_VER=1.98.2 .\n',
		'flake.nix': `inputs.nixpkgs.url = "github:NixOS/nixpkgs/${oldRevision}";\n# Keep other settings\n`,
		'flake.lock': JSON.stringify({ nodes: { nixpkgs: { locked: { rev: oldRevision } } } }),
	};
	for (const [name, content] of Object.entries(files)) writeFileSync(join(root, name), content);
	/** @type {string[][]} */
	const calls = [];
	const run = (/** @type {string} */ command, /** @type {string[]} */ args) => {
		calls.push([command, ...args]);
		if (command === 'gh') {
			if (args[1].includes('/commits?')) return newRevision;
			if (missingFamily && args[1].includes(oldRevision)) throw new Error('HTTP 404: compiler family absent');
			const matches = available && (currentMatches || args[1].includes(newRevision));
			return `version = "${matches ? '1.99.3' : '1.98.2'}";`;
		}
		assert.equal(command, 'nix');
		if (args[0] === 'eval' && missingAttribute) throw new Error('Nix compiler attribute missing');
		if (args[0] === 'eval') return mismatch && args[2].includes('aarch64-darwin') ? '1.99.2' : '1.99.3';
		if (args[1] === 'lock') {
			const lock = { nodes: { nixpkgs: { locked: { rev: currentMatches ? oldRevision : newRevision } } } };
			writeFileSync(join(root, 'flake.lock'), JSON.stringify(lock));
		}
		if (args[1] === 'check' && checkFails) throw new Error('flake validation failed');
		return '';
	};
	return { root, run, files, calls };
}

test('updates every toolchain surface after verifying all platform compilers', t => {
	const f = fixture(t);
	assert.equal(updateToolchain('go1.99.3', f), newRevision);
	assert.match(readFileSync(join(f.root, 'go.mod'), 'utf8'), /go 1\.26\.0\n\ntoolchain go1\.99\.3/);
	assert.match(readFileSync(join(f.root, 'Dockerfile'), 'utf8'), /GOLANG_VER=1\.99\.3/);
	assert.match(readFileSync(join(f.root, 'CONTRIBUTING.md'), 'utf8'), /GOLANG_VER=1\.99\.3/);
	assert.equal(readFileSync(join(f.root, 'flake.nix'), 'utf8'), f.files['flake.nix'].replace(oldRevision, newRevision));
	const evals = f.calls.filter(call => call[0] === 'nix' && call[1] === 'eval');
	assert.equal(evals.length, 3);
	for (const call of evals) assert.match(call[3], /\.go_1_99\.version$/);
	assert.deepEqual(f.calls.at(-1), ['nix', 'flake', 'check', '--all-systems', '--no-build', '--no-update-lock-file']);
});

for (
	const [name, options, message] of /** @type {[string, {available?: boolean, mismatch?: boolean}, RegExp][]} */ ([
		['unavailable target', { available: false }, /No nixpkgs compiler provides/],
		['platform compiler mismatch', { mismatch: true }, /aarch64-darwin provides Go/],
	])
) {
	test(`${name} rejects without modifying files`, t => {
		const f = fixture(t, options);
		assert.throws(() => updateToolchain('go1.99.3', f), message);
		for (const [name, before] of Object.entries(f.files)) {
			assert.equal(readFileSync(join(f.root, name), 'utf8'), before);
		}
		assert.equal(f.calls.some(call => call[1] === 'flake'), false);
	});
}

test('keeps a matching pin without querying new commits', t => {
	const f = fixture(t, { currentMatches: true });
	assert.equal(updateToolchain('go1.99.3', f), oldRevision);
	assert.equal(f.calls.some(call => call[2]?.includes('/commits?')), false);
});

test('failed final flake validation fails the update', t => {
	assert.throws(() => updateToolchain('go1.99.3', fixture(t, { checkFails: true })), /flake validation failed/);
});

test('a family absent from the old pin can resolve from a new compiler commit', t => {
	const f = fixture(t, { missingFamily: true });
	assert.equal(updateToolchain('go1.99.3', f), newRevision);
});

test('a missing compiler attribute rejects before modifying files', t => {
	const f = fixture(t, { missingAttribute: true });
	assert.throws(() => updateToolchain('go1.99.3', f), /Nix compiler attribute missing/);
	for (const [name, before] of Object.entries(f.files)) assert.equal(readFileSync(join(f.root, name), 'utf8'), before);
});

test('invalid target rejects before external commands or file writes', t => {
	const f = fixture(t);
	assert.throws(() => updateToolchain('latest', f), /Invalid Go toolchain/);
	assert.deepEqual(f.calls, []);
});

test('upkeep publishes all synchronized files and validates before creating the PR', () => {
	const workflow = readFileSync(new URL('../.github/workflows/upkeep.yml', import.meta.url), 'utf8');
	const job = workflow.split('  go-toolchain:\n')[1].split('  shellcheck:\n')[0];
	assert.match(job, /node scripts\/update-go-toolchain\.mjs "\$\{version\}"/);
	assert.match(job, /add-paths: \|\n\s+go\.mod\n\s+Dockerfile\n\s+CONTRIBUTING\.md\n\s+flake\.nix\n\s+flake\.lock/);
	assert.ok(job.indexOf('node scripts/update-go-toolchain.mjs') < job.indexOf('peter-evans/create-pull-request'));
});
