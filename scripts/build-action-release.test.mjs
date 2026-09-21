import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

import { assertOutsideCheckout, createReleaseCommit, prepareRelease } from './build-action-release.mjs';

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
		await mkdir(join(output, 'action', 'dist'), { recursive: true });
		/** @param {string[]} args */
		const git = (args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
		git(['init', '--quiet']);
		git(['config', 'user.name', 'Release test']);
		git(['config', 'user.email', 'release-test@example.invalid']);
		git(['config', 'commit.gpgsign', 'false']);
		await writeFile(join(root, 'source.txt'), 'original\n');
		git(['add', 'source.txt']);
		git(['commit', '--quiet', '--message', 'Source fixture']);
		const parent = git(['rev-parse', 'HEAD']);
		await writeFile(join(root, 'source.txt'), 'staged\n');
		git(['add', 'source.txt']);
		await writeFile(join(root, 'source.txt'), 'unstaged\n');
		const index = await readFile(join(root, '.git', 'index'));
		const bundle = 'export {};\n';
		const digest = createHash('sha256').update(bundle).digest('hex');
		for (
			const [name, content] of Object.entries({
				'action.yml': 'runs:\n  using: node24\n  main: dist/main.mjs\n',
				'README.md': '# Release fixture\n',
				'LICENSE.txt': 'License fixture\n',
				'dist/main.mjs': bundle,
				'SHA256SUMS': `${digest}  dist/main.mjs\n`,
			})
		) await writeFile(join(output, 'action', name), content);
		const commit = await createReleaseCommit(root, output, '1.17.0', parent);
		assert.equal(git(['rev-parse', 'HEAD']), parent);
		assert.equal(git(['rev-parse', `${commit}^`]), parent);
		assert.equal(await readFile(join(root, 'source.txt'), 'utf8'), 'unstaged\n');
		assert.deepEqual(await readFile(join(root, '.git', 'index')), index);
		assert.deepEqual(git(['ls-tree', '-r', '--name-only', commit]).split('\n'), [
			'LICENSE.txt',
			'README.md',
			'SHA256SUMS',
			'action.yml',
			'dist/main.mjs',
		]);
		assert.equal(git(['show', `${commit}:dist/main.mjs`]), bundle.trim());
		await writeFile(join(output, 'action', 'dist/main.mjs'), 'corrupted');
		await assert.rejects(createReleaseCommit(root, output, '1.17.0', parent), /checksum does not match/);
	} finally {
		if (dirname(temporary) === tempRoot) await rm(temporary, { recursive: true, force: true });
	}
});
