import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checksumForAsset, nativeAssetName, runnerPlatform } from '#assets';

test('native release selection supports each JavaScript runner platform', () => {
	for (
		const entry of [
			{ os: 'linux', arch: 'x64', native: 'linux_amd64.tar.gz' },
			{ os: 'linux', arch: 'arm64', native: 'linux_arm64.tar.gz' },
			{ os: 'darwin', arch: 'x64', native: 'darwin_amd64.tar.gz' },
			{ os: 'darwin', arch: 'arm64', native: 'darwin_arm64.tar.gz' },
			{ os: 'win32', arch: 'x64', native: 'windows_amd64.zip' },
			{ os: 'win32', arch: 'arm64', native: 'windows_arm64.zip' },
		]
	) {
		const platform = runnerPlatform(entry.os, entry.arch);
		assert.equal(nativeAssetName('1.17.0', platform), `actionlint_1.17.0_${entry.native}`);
	}
	assert.throws(() => runnerPlatform('freebsd', 'x64'), /Unsupported runner operating system/);
	assert.throws(() => runnerPlatform('linux', 'ia32'), /Unsupported runner architecture/);
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
