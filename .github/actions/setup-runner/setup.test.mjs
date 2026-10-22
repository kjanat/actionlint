import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { release } from './proofs.mjs';
import {
	assetFor,
	cacheKey,
	identity,
	inspect,
	install,
	pinnedVersion,
	publish,
	seal,
	verificationMode,
} from './setup.mjs';

/** @param {string | Uint8Array} bytes */
const digest = bytes => createHash('sha256').update(bytes).digest('hex');

/** @param {import('node:test').TestContext} t @param {string} [platform] */
function fixture(t, platform = 'linux-x64-gnu') {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-runner-cache-'));
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const expected = {
		...identity('0.28.0', 'auto', platform),
		runnerSHA256: digest('verified runner bytes'),
		runSHA256: digest('verified run bytes'),
	};
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
		const forged = { ...f.expected, runner: digest('substituted bytes'), run: digest('substituted bytes') };
		writeFileSync(join(f.cached, 'receipt.json'), JSON.stringify(forged));
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
	assert.equal(f.expected.installer, `${release.repository}@${release.sourceDigest}`);
	assert.ok(!metadata.includes('uses: kjanat/runner@'));
	assert.ok(metadata.includes("if: steps.inspect.outputs.hit != 'true'"));
	assert.throws(() => identity('0.29.0', 'require', 'linux-x64-gnu'), /reviewed offline provenance/);
	assert.throws(() => identity('0.28.0', 'require', 'linux-x64-musl'), /reviewed offline provenance/);
});

test('real release installs with offline provenance, authenticates cache and rejects invalid evidence', async t => {
	const root = mkdtempSync(join(tmpdir(), 'actionlint-runner-proof-'));
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const platform = `${process.platform}-${process.arch}${process.platform === 'linux' ? '-gnu' : ''}`;
	const asset = assetFor('0.28.0', platform);
	const response = await fetch(asset.url);
	assert.equal(response.status, 200);
	const archive = await response.arrayBuffer();
	/** @type {typeof fetch} */
	const request = async url => {
		assert.equal(url, asset.url);
		return new Response(archive);
	};
	/** @type {string[]} */
	const commands = [];
	/** @type {import('./setup.mjs').Execute} */
	const execute = (file, args, options) => {
		commands.push(file);
		if (file === 'gh') {
			assert.equal(options.env.GH_TOKEN, '');
			assert.equal(options.env.GITHUB_TOKEN, '');
			assert.equal(options.env.HTTPS_PROXY, 'http://127.0.0.1:1');
			assert.deepEqual(args.slice(-2), [release.predicateType, '--deny-self-hosted-runners']);
		}
		return execFileSync(file, args, options);
	};
	for (const mode of ['auto', 'require']) {
		const expected = identity('0.28.0', verificationMode(mode), platform);
		const directory = join(root, mode);
		await install(directory, expected, { request, execute });
		assert.equal(inspect(directory, expected), true);
		assert.equal(publish(directory, expected).version, '0.28.0');
		await install(directory, expected, {
			request: async () => assert.fail('warm cache used network'),
			execute: () => assert.fail('warm cache used provenance or extraction subprocess'),
		});
		const binary = platform.startsWith('win32-') ? 'runner.exe' : 'runner';
		writeFileSync(join(directory, binary), 'corrupt cache');
		await install(directory, expected, { request, execute });
		assert.equal(inspect(directory, expected), true);
	}
	assert.deepEqual(commands, ['gh', 'tar', 'gh', 'tar', 'gh', 'tar', 'gh', 'tar']);
	const expected = identity('0.28.0', 'require', platform);
	await t.test('archive digest and size fail before provenance or extraction', async () => {
		for (const bytes of [new Uint8Array(archive.byteLength), new Uint8Array(archive.byteLength + 1)]) {
			await assert.rejects(
				install(join(root, 'bad-archive'), expected, {
					request: async () => new Response(bytes),
					execute: () => assert.fail('untrusted archive reached execution'),
				}),
				/archive.*(?:mismatch|size)/,
			);
		}
	});
	await t.test('invalid signed bundles and roots fail before extraction', async () => {
		const proofs = join(root, 'proofs');
		const original = fileURLToPath(new URL('proofs/0.28.0/', import.meta.url));
		for (const bad of ['signature', 'subject', 'root']) {
			cpSync(original, proofs, { recursive: true });
			if (bad === 'root') writeFileSync(join(proofs, 'trusted-root.jsonl'), '{}');
			else if (bad === 'subject') {
				const other = Object.values(release.assets).find(candidate => candidate.package !== asset.package);
				assert.ok(other);
				cpSync(join(original, other.bundle), join(proofs, asset.bundle));
			} else {
				const file = join(proofs, asset.bundle);
				const original = readFileSync(file, 'utf8');
				const tampered = original.replace(/("sig"\s*:\s*")[A-Za-z0-9+/]/, '$1A');
				assert.notEqual(tampered, original);
				writeFileSync(file, tampered);
			}
			await assert.rejects(install(join(root, `bad-${bad}`), expected, {
				request,
				proofs,
				execute: (file, args, options) => {
					assert.equal(file, 'gh', 'invalid provenance reached extraction');
					return execute(file, args, options);
				},
			}));
		}
	});
});
