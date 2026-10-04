import assert from 'node:assert/strict';
import { test } from 'node:test';

import shellcheck from '#tools/shellcheck' with { type: 'json' };

import { checksumForAsset, nativeAssetName, runnerPlatform, shellcheckAsset } from '#assets';

test('native release selection supports each JavaScript runner platform', () => {
	for (
		const entry of [
			{ os: 'linux', arch: 'x64', native: 'linux_amd64.tar.gz', shellcheck: 'linux.x86_64.tar.gz' },
			{ os: 'linux', arch: 'arm64', native: 'linux_arm64.tar.gz', shellcheck: 'linux.aarch64.tar.gz' },
			{ os: 'darwin', arch: 'x64', native: 'darwin_amd64.tar.gz', shellcheck: 'darwin.x86_64.tar.gz' },
			{ os: 'darwin', arch: 'arm64', native: 'darwin_arm64.tar.gz', shellcheck: 'darwin.aarch64.tar.gz' },
			{ os: 'win32', arch: 'x64', native: 'windows_amd64.zip', shellcheck: 'zip' },
			{ os: 'win32', arch: 'arm64', native: 'windows_arm64.zip', shellcheck: 'zip' },
		]
	) {
		const platform = runnerPlatform(entry.os, entry.arch);
		assert.equal(nativeAssetName('1.17.0', platform), `actionlint_1.17.0_${entry.native}`);
		const expectedName = `shellcheck-${shellcheck.tagName}.${entry.shellcheck}`;
		const upstream = shellcheck.assets.find((asset) => asset.name === expectedName);
		assert.ok(upstream);
		const selected = shellcheckAsset(platform);
		assert.equal(selected.name, expectedName);
		assert.equal(selected.url, upstream.url);
		assert.equal(`sha256:${selected.sha256}`, upstream.digest);
	}
	assert.throws(() => runnerPlatform('freebsd', 'x64'), /Unsupported runner operating system/);
	assert.throws(() => runnerPlatform('linux', 'ia32'), /Unsupported runner architecture/);
});

test('a refreshed ShellCheck release needs no version, URL, or digest changes in code', () => {
	const asset = {
		name: 'shellcheck-v9.8.7.linux.x86_64.tar.gz',
		url: 'https://example.invalid/new-release/shellcheck.tar.gz',
		digest: `sha256:${'a'.repeat(64)}`,
	};
	const platform = runnerPlatform('linux', 'x64');
	const release = { tagName: 'v9.8.7', assets: [asset] };
	assert.deepEqual(shellcheckAsset(platform, release), {
		name: asset.name,
		url: asset.url,
		sha256: 'a'.repeat(64),
		archive: 'tar.gz',
	});
	assert.throws(() => shellcheckAsset(platform, { ...release, assets: [] }), /Missing ShellCheck release asset/);
	for (const digest of [null, '', 'sha256:bad', `sha512:${'a'.repeat(64)}`]) {
		assert.throws(
			() => shellcheckAsset(platform, { ...release, assets: [{ ...asset, digest }] }),
			/Missing or invalid SHA-256 digest/,
		);
	}
});

test('checksum lookup matches the exact archive and rejects ambiguous manifests', () => {
	const digest = 'a'.repeat(64);
	const other = 'b'.repeat(64);
	const filename = 'actionlint_1.17.0_linux_amd64.tar.gz';
	assert.equal(checksumForAsset(`${other}  other.zip\r\n${digest} *${filename}\r\n`, filename), digest);
	assert.throws(() => checksumForAsset(`${digest}  prefix-${filename}\n`, filename), /Missing/);
	assert.throws(() => checksumForAsset(`invalid  ${filename}\n`, filename), /Missing/);
	assert.throws(() => checksumForAsset(`${digest}  ${filename}\n${other}  ${filename}\n`, filename), /Duplicate/);
});
