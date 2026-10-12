import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

import { assertOutsideCheckout, createReleaseCommit, prepareRelease, recordRelease } from './build-action-release.mjs';

test('release output cannot create generated files in the source checkout', () => {
	const root = resolve('checkout');
	assert.throws(() => assertOutsideCheckout(root, 'relative'), /must be absolute/);
	assert.throws(() => assertOutsideCheckout(root, root), /outside the source checkout/);
	assert.throws(() => assertOutsideCheckout(root, join(root, 'dist')), /outside the source checkout/);
	assertOutsideCheckout(root, resolve('release'));
});

test('a symlink cannot redirect release output into the checkout', async () => {
	const tempRoot = resolve(tmpdir());
	const temporary = await mkdtemp(join(tempRoot, 'actionlint-release-path-test-'));
	try {
		const root = join(temporary, 'source');
		await mkdir(root);
		const link = join(temporary, 'linked-source');
		await symlink(root, link, process.platform === 'win32' ? 'junction' : 'dir');
		await assert.rejects(prepareRelease(root, join(link, 'generated'), '1.17.0'), /outside the source checkout/);
		await assert.rejects(stat(join(root, 'generated')), { code: 'ENOENT' });
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
});

test('release commit keeps source HEAD, working files and staging index intact', async () => {
	const tempRoot = resolve(tmpdir());
	const temporary = await mkdtemp(join(tempRoot, 'actionlint-release-test-'));
	try {
		const root = join(temporary, 'source');
		const output = join(temporary, 'release');
		await mkdir(root);
		await mkdir(join(output, 'action'), { recursive: true });
		/** @param {string[]} args */
		const git = (args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
		git(['init', '--quiet']);
		git(['config', 'user.name', 'Release test']);
		git(['config', 'user.email', 'release-test@example.invalid']);
		git(['config', 'commit.gpgsign', 'false']);
		git(['config', 'tag.gpgsign', 'false']);
		git(['config', 'core.autocrlf', 'true']);
		const sourceFiles = {
			'.gitattributes': await readFile(new URL('../.gitattributes', import.meta.url), 'utf8'),
			'action.yml': 'runs:\n  using: node24\n  main: action.mjs\n',
			'README.md': '# Release fixture\n',
			'LICENSE.txt': 'License fixture\n',
			'go.mod': 'module example.invalid/release\n',
		};
		for (const [name, content] of Object.entries(sourceFiles)) await writeFile(join(root, name), content);
		await writeFile(join(root, 'source.txt'), 'original\n');
		git(['add', '.']);
		git(['commit', '--quiet', '--message', 'Source fixture']);
		const parent = git(['rev-parse', 'HEAD']);
		await writeFile(join(root, 'source.txt'), 'staged\n');
		git(['add', 'source.txt']);
		await writeFile(join(root, 'source.txt'), 'unstaged\n');
		const index = await readFile(join(root, '.git', 'index'));
		const bundle = 'console.log("bundle from normal version tag");\n';
		const digest = createHash('sha256').update(bundle).digest('hex');
		for (
			const [name, content] of Object.entries({
				...sourceFiles,
				'action.mjs': bundle,
				'SHA256SUMS': `${digest}  action.mjs\n`,
			})
		) await writeFile(join(output, 'action', name), content);
		const commit = await createReleaseCommit(root, output, '1.17.0', parent);
		assert.equal(git(['rev-parse', 'HEAD']), parent);
		assert.equal(git(['rev-parse', `${commit}^`]), parent);
		assert.equal(await readFile(join(root, 'source.txt'), 'utf8'), 'unstaged\n');
		assert.deepEqual(await readFile(join(root, '.git', 'index')), index);
		assert.deepEqual(git(['ls-tree', '-r', '--name-only', commit]).split('\n'), [
			'.gitattributes',
			'LICENSE.txt',
			'README.md',
			'SHA256SUMS',
			'action.mjs',
			'action.yml',
			'go.mod',
			'source.txt',
		]);
		assert.equal(git(['show', `${commit}:source.txt`]), 'original');
		assert.equal(git(['show', `${commit}:go.mod`]), sourceFiles['go.mod'].trim());
		assert.equal(git(['show', `${commit}:action.mjs`]), bundle.trim());

		git(['tag', 'v1.17.0', commit]);
		assert.throws(() => recordRelease(root, 'v1.17.0', parent), /checkout changed/);
		const integration = join(temporary, 'integration');
		git(['clone', '--quiet', '--no-local', root, integration]);
		/** @param {string[]} args */
		const sourceGit = (args) => execFileSync('git', args, { cwd: integration, encoding: 'utf8' }).trim();
		sourceGit(['config', 'user.name', 'Release test']);
		sourceGit(['config', 'user.email', 'release-test@example.invalid']);
		sourceGit(['config', 'commit.gpgsign', 'false']);
		recordRelease(integration, 'v1.17.0', parent);
		assert.equal(sourceGit(['rev-parse', 'HEAD^{tree}']), git(['rev-parse', `${parent}^{tree}`]));
		assert.equal(sourceGit(['rev-parse', 'HEAD^1']), parent);
		assert.equal(sourceGit(['rev-parse', 'HEAD^2']), commit);
		assert.equal(sourceGit(['status', '--porcelain']), '');
		assert.equal(
			sourceGit(['describe', '--tags', '--abbrev=0', '--match', 'v[0-9]*.[0-9]*.[0-9]*', '--exclude', '*-*']),
			'v1.17.0',
		);
		await assert.rejects(stat(join(integration, 'action.mjs')), { code: 'ENOENT' });
		assert.throws(() => recordRelease(integration, 'v1.17.0', parent), /HEAD changed/);

		const checkout = join(temporary, 'tagged-checkout');
		git([
			'-c',
			'advice.detachedHead=false',
			'clone',
			'--quiet',
			'--no-local',
			'--config',
			'core.autocrlf=true',
			'--branch',
			'v1.17.0',
			root,
			checkout,
		]);
		assert.equal(await readFile(join(checkout, 'action.mjs'), 'utf8'), bundle);
		assert.equal(await readFile(join(checkout, 'SHA256SUMS'), 'utf8'), `${digest}  action.mjs\n`);

		const consumer = join(temporary, 'consumer');
		await mkdir(consumer);
		const archive = join(temporary, 'release.tar');
		git(['archive', '--format=tar', '--output', archive, 'v1.17.0']);
		execFileSync('tar', ['-xf', '-'], { cwd: consumer, input: await readFile(archive) });
		const metadata = await readFile(join(consumer, 'action.yml'), 'utf8');
		const main = /^\s+main: (.+)$/m.exec(metadata)?.[1];
		assert.ok(main, 'version tag must include JavaScript Action metadata');
		assert.equal(
			execFileSync(process.execPath, [join(consumer, main)], { encoding: 'utf8' }).trim(),
			'bundle from normal version tag',
		);
		const published = join(temporary, 'published');
		await prepareRelease(consumer, published, '1.17.0', true);
		assert.equal(await readFile(join(published, 'assets/actionlint-action_1.17.0.mjs'), 'utf8'), bundle);
		assert.equal(await readFile(join(published, 'action/action.mjs'), 'utf8'), bundle);
		await assert.rejects(prepareRelease(root, join(temporary, 'source-only'), '1.17.0', true), { code: 'ENOENT' });

		await writeFile(join(output, 'action', 'action.yml'), 'runs: { using: node24, main: missing.mjs }\n');
		await assert.rejects(createReleaseCommit(root, output, '1.17.0', parent), /differs from the source commit/);
		await writeFile(join(output, 'action', 'action.yml'), sourceFiles['action.yml']);
		await writeFile(join(output, 'action', 'action.mjs'), 'corrupted');
		await assert.rejects(createReleaseCommit(root, output, '1.17.0', parent), /checksum does not match/);
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
});
