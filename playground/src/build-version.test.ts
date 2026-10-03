import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { buildVersion, renderBuildVersion } from './build-version.ts';

test('playground keeps source identity separate from the published release after promotion', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-playground-version-'));
	try {
		const git = (...args: string[]) =>
			execFileSync('git', ['-c', 'commit.gpgsign=false', '-c', `core.hooksPath=${directory}`, ...args], {
				cwd: directory,
				encoding: 'utf8',
				stdio: ['ignore', 'pipe', 'ignore'],
				env: {
					...process.env,
					GIT_AUTHOR_NAME: 'Test',
					GIT_AUTHOR_EMAIL: 'test@example.com',
					GIT_COMMITTER_NAME: 'Test',
					GIT_COMMITTER_EMAIL: 'test@example.com',
				},
			}).trim();
		git('init', '--initial-branch=main');
		await writeFile(join(directory, 'source.txt'), 'source\n');
		git('add', 'source.txt');
		git('commit', '-m', 'Source');
		git('switch', '-c', 'candidate');
		await writeFile(join(directory, 'action.mjs'), '// bundled release\n');
		git('add', 'action.mjs');
		git('commit', '-m', 'Candidate');
		git('-c', 'tag.gpgsign=false', 'tag', 'v1.2.3');
		const candidate = buildVersion(directory, 'v1.2.3');
		assert.equal(candidate.version, 'v1.2.3');
		assert.equal(candidate.url, candidate.release?.url);
		git('switch', 'main');
		git('merge', '--strategy=ours', '--no-ff', 'candidate', '-m', 'Record bundled release');
		const identity = buildVersion(directory, 'v1.2.3');
		assert.match(identity.version, /^v1\.2\.3-\d+-g[0-9a-f]+$/);
		assert.equal(identity.ref, git('rev-parse', 'HEAD'));
		assert.notEqual(identity.ref, candidate.ref);
		assert.equal(identity.url, `https://github.com/kjanat/actionlint/tree/${identity.ref}`);
		assert.deepEqual(identity.release, {
			version: 'v1.2.3',
			url: 'https://github.com/kjanat/actionlint/releases/tag/v1.2.3',
		});
		const template = await readFile(new URL('../index.html', import.meta.url), 'utf8');
		const rendered = renderBuildVersion(template, identity);
		assert.ok(rendered.includes(`Source ${identity.version}`));
		assert.ok(rendered.includes('Latest release v1.2.3'));
		assert.ok(rendered.includes(`/blob/${identity.ref}/docs/checks.md`));
		assert.ok(!rendered.includes('%ACTIONLINT_'));
		const local = renderBuildVersion(template, buildVersion(directory));
		assert.ok(!local.includes('Latest release'));
		assert.ok(!local.includes('%ACTIONLINT_'));
		await writeFile(join(directory, 'source.txt'), 'changed\n');
		const dirty = buildVersion(directory, 'v1.2.3');
		assert.match(dirty.version, /-dirty$/);
		assert.equal(dirty.url, identity.url);
		assert.deepEqual(dirty.release, identity.release);
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('playground retains development fallback and rejects malformed release metadata', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-playground-no-git-'));
	try {
		const identity = buildVersion(directory, 'v1.2.3');
		assert.equal(identity.version, 'development');
		assert.equal(identity.ref, 'HEAD');
		assert.equal(identity.release?.version, 'v1.2.3');
		for (const tag of ['main', 'v01.2.3', 'v1.2.3\n', 'v1.2.3"><script>']) {
			assert.throws(() => buildVersion(directory, tag), /Invalid latest published release tag/);
		}
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});
