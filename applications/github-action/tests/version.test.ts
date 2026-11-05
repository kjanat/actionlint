import assert from 'node:assert/strict';
import { test } from 'node:test';

import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { InputError } from '#runtime';
import { releaseVersion, selectedVersion, versionFromFile } from '#version';

test('exact releases preserve prereleases and reject ranges and paths', () => {
	for (const value of ['1.17.0', 'v1.17.0', '  v1.17.0  ']) assert.equal(releaseVersion(value), '1.17.0');
	assert.equal(releaseVersion('v2.0.0-rc.1+build.2'), '2.0.0-rc.1+build.2');
	for (
		const value of ['', 'latest', 'v1', '^1.17.0', '1.017.0', '1.17.0-01', '1.17.0-rc.01', '../1.17.0', '1.17.0\n2.0.0']
	) {
		assert.throws(() => releaseVersion(value), InputError);
	}
});

test('version files select actionlint among tools, with comments and CRLF', () => {
	assert.equal(
		versionFromFile('\uFEFF# tools\r\nnodejs 24.0.0\r\nactionlint v1.17.0 # pinned\r\n', '.tool-versions'),
		'1.17.0',
	);
	assert.equal(versionFromFile('actionlint 1.17.0\nnodejs 24', 'custom-tool-versions'), '1.17.0');
	assert.equal(versionFromFile('v1.17.0\n# pin\n', '.actionlint-version'), '1.17.0');
	for (
		const content of [
			'nodejs 24',
			'actionlint',
			'actionlint 1.17.0 system',
			'actionlint system',
			'actionlint 1.17.0\nactionlint 1.17.1',
		]
	) {
		assert.throws(() => versionFromFile(content, '.tool-versions'), InputError);
	}
});

test('explicit version wins; files resolve from workspace and working-directory', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-version-'));
	try {
		await mkdir(join(directory, 'project'));
		await writeFile(join(directory, 'project', '.tool-versions'), 'actionlint 1.17.0\n');
		const environment = {
			GITHUB_WORKSPACE: directory,
			'INPUT_WORKING-DIRECTORY': 'project',
			'INPUT_VERSION-FILE': '.tool-versions',
		};
		assert.equal(await selectedVersion(environment, '2.0.0'), '1.17.0');
		assert.equal(
			await selectedVersion({ ...environment, INPUT_VERSION: 'v1.18.0', 'INPUT_VERSION-FILE': 'missing' }, '2.0.0'),
			'1.18.0',
		);
		assert.equal(await selectedVersion({}, '2.0.0'), '2.0.0');
		await assert.rejects(
			selectedVersion({ ...environment, 'INPUT_VERSION-FILE': 'missing' }, '2.0.0'),
			/Cannot read version-file/,
		);
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});
