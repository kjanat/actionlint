import assert from 'node:assert/strict';
import { test } from 'node:test';

import { parse } from 'yaml';

import { generateManifests, manifestFileNames, renderManifests } from '#generate';
import { installerManifest } from '#schema';

test('portable WinGet manifest validates the CLI without accepting lint failures as installer successes', () => {
	const version = '1.17.0';
	const checksums = [
		`${'a'.repeat(64)}  actionlint_${version}_windows_386.zip`,
		`${'b'.repeat(64)}  actionlint_${version}_windows_amd64.zip`,
		`${'c'.repeat(64)}  actionlint_${version}_windows_arm64.zip`,
	].join('\n');
	const manifests = generateManifests({
		version,
		releaseDate: '2026-09-13',
		checksums,
		releaseNotes: 'Release notes for testing.',
	});
	const { installer } = manifests;

	assert.equal(installer.InstallerType, 'zip');
	assert.equal(installer.NestedInstallerType, 'portable');
	assert.deepEqual(installer.NestedInstallerFiles, [{
		RelativeFilePath: 'actionlint.exe',
		PortableCommandAlias: 'actionlint',
	}]);
	assert.deepEqual(installer.InstallationMetadata?.Files, [{
		RelativeFilePath: 'actionlint.exe',
		FileType: 'launch',
		InvocationParameter: '-version',
		DisplayName: 'actionlint',
	}]);
	assert.equal(Object.hasOwn(installer, 'InstallerSuccessCodes'), false);

	const installerYaml = renderManifests(manifests)[manifestFileNames.installer];
	assert.ok(installerYaml);
	assert.doesNotMatch(installerYaml, /^InstallerSuccessCodes:/m);
	assert.deepEqual(installerManifest.parse(parse(installerYaml)), installer);
});
