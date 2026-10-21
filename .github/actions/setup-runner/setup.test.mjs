import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { cacheKey, identity, inspect, pinnedVersion, publish, seal, verificationMode } from './setup.mjs';

/** @param {import('node:test').TestContext} t @param {string} [platform] */
function fixture(t, platform = 'linux-x64-gnu') {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-runner-cache-'));
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const expected = identity('0.28.0', 'auto', platform);
	const installed = join(root, 'upstream');
	const cached = join(root, 'cached');
	mkdirSync(installed);
	const suffix = platform.startsWith('win32-') ? '.exe' : '';
	writeFileSync(join(installed, `runner${suffix}`), 'verified runner bytes');
	writeFileSync(join(installed, `run${suffix}`), 'verified run bytes');
	return { expected, installed, cached, suffix };
}

test('cold installations require a completed matching upstream result; warm reuse needs no network', t => {
	const f = fixture(t);
	const network = t.mock.method(globalThis, 'fetch', () => {
		throw new Error('network unavailable');
	});
	assert.equal(inspect(f.cached, f.expected), false);
	assert.throws(() => publish(f.cached, f.expected), /verification/);
	assert.throws(() => seal(f.installed, f.cached, f.expected, '0.27.0'), /different version/);
	assert.equal(inspect(f.cached, f.expected), false);
	seal(f.installed, f.cached, f.expected, '0.28.0');
	assert.equal(inspect(f.cached, f.expected), true);
	/** @type {{file: string, args: string[]}[]} */
	const executions = [];
	const outputs = publish(f.cached, f.expected, (file, args) => {
		executions.push({ file, args });
		return 'runner 0.28.0\nchannel: stable\n';
	});
	assert.deepEqual(outputs, {
		version: '0.28.0',
		'bin-dir': f.cached,
		'runner-bin': join(f.cached, 'runner'),
		'run-bin': join(f.cached, 'run'),
	});
	assert.deepEqual(executions, [{ file: join(f.cached, 'runner'), args: ['--version'] }]);
	assert.equal(network.mock.callCount(), 0);
});

test('corrupt cached binaries are rejected before either executable can run', t => {
	const f = fixture(t);
	for (const binary of ['runner', 'run']) {
		seal(f.installed, f.cached, f.expected, '0.28.0');
		writeFileSync(join(f.cached, binary), 'substituted bytes');
		assert.equal(inspect(f.cached, f.expected), false, binary);
		assert.throws(() =>
			publish(f.cached, f.expected, () => {
				assert.fail('corrupt executable reached execution');
			}), /verification/);
	}
});

test('receipts bind installer source, version, ABI platform and verification policy', t => {
	const f = fixture(t);
	seal(f.installed, f.cached, f.expected, '0.28.0');
	for (
		const expected of [
			{ ...f.expected, version: '0.29.0' },
			{ ...f.expected, platform: 'linux-arm64-gnu' },
			{ ...f.expected, platform: 'linux-x64-musl' },
			{ ...f.expected, installer: `kjanat/runner@${'0'.repeat(40)}` },
			{ ...f.expected, source: 'different helper source' },
			identity('0.28.0', 'require', f.expected.platform),
		]
	) {
		assert.notEqual(cacheKey(expected), cacheKey(f.expected));
		assert.equal(inspect(f.cached, expected), false);
		assert.throws(() => publish(f.cached, expected), /verification/);
	}
	assert.equal(inspect(f.cached, f.expected), true);
	writeFileSync(join(f.cached, 'receipt.json'), 'broken JSON');
	assert.equal(inspect(f.cached, f.expected), false);
});

test('Windows installation outputs preserve executable suffixes', t => {
	const f = fixture(t, 'win32-arm64');
	seal(f.installed, f.cached, f.expected, '0.28.0');
	assert.equal(inspect(f.cached, f.expected), true);
	const outputs = publish(f.cached, f.expected, () => 'runner 0.28.0');
	assert.equal(outputs['runner-bin'], join(f.cached, 'runner.exe'));
	assert.equal(outputs['run-bin'], join(f.cached, 'run.exe'));
	assert.throws(() => publish(f.cached, f.expected, () => 'runner 0.27.0'), /different version/);
});

test('CLI preparation and cache inspection use GitHub file commands', t => {
	const f = fixture(t);
	const file = join(f.installed, '.tool-versions');
	const output = join(f.installed, 'output');
	writeFileSync(file, 'runner 0.28.0\n');
	writeFileSync(output, '');
	const env = {
		...process.env,
		VERSION_FILE: file,
		VERIFY_MODE: 'auto',
		GITHUB_OUTPUT: output,
		RUNNER_TEMP: f.installed,
	};
	const script = new URL('setup.mjs', import.meta.url);
	execFileSync(process.execPath, [script.pathname.replace(/^\/(?=[A-Za-z]:)/, ''), 'prepare'], { env });
	const prepared = readFileSync(output, 'utf8');
	assert.match(prepared, /^version=0\.28\.0\ncache-dir=.+\ncache-key=actionlint-runner-[0-9a-f]{64}\n$/);
	writeFileSync(output, '');
	execFileSync(process.execPath, [script.pathname.replace(/^\/(?=[A-Za-z]:)/, ''), 'inspect'], { env });
	assert.equal(readFileSync(output, 'utf8'), 'hit=false\n');
});

test('CI pins normalize runner tool aliases; floating versions and verification downgrade fail', t => {
	const f = fixture(t);
	const file = join(f.installed, '.tool-versions');
	for (
		const tool of ['runner', 'runner-run', 'github:kjanat/runner', 'github:kjanat/runner[exe=runner]', 'npm:runner-run']
	) {
		writeFileSync(file, `go 1.27\n${tool} v0.28.0 0.27.0 # fallback\n`);
		assert.equal(pinnedVersion(file), '0.28.0', tool);
	}
	for (const version of ['latest', '0.28', '../tool', 'system']) {
		writeFileSync(file, `runner ${version}\n`);
		assert.throws(() => pinnedVersion(file), /exact version pin/);
	}
	writeFileSync(file, 'go 1.27\n');
	assert.throws(() => pinnedVersion(file), /no runner version/);
	assert.throws(() => verificationMode('off'), /auto or require/);
	assert.equal(verificationMode('require'), 'require');
	const metadata = readFileSync(new URL('action.yml', import.meta.url), 'utf8');
	assert.ok(metadata.includes(`uses: ${f.expected.installer}`));
	assert.ok(metadata.includes("if: steps.inspect.outputs.hit != 'true'"));
});
