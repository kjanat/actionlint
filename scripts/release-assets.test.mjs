import assert from 'node:assert/strict';
import test from 'node:test';

import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

import { collectAssets, publishManifests } from './release-assets.mjs';

/** @param {import('node:test').TestContext} t */
async function fixture(t) {
	const tempRoot = resolve(tmpdir());
	const root = await mkdtemp(join(tempRoot, 'actionlint-release-assets-'));
	t.after(async () => {
		if (dirname(root) === tempRoot) await rm(root, { recursive: true, force: true });
	});
	return root;
}

test('collect preserves release bytes and generated package manifests without uploading build metadata', async (t) => {
	const root = await fixture(t);
	const output = join(root, 'assets');
	await mkdir(output);
	const files = [
		'homebrew/Casks/actionlint.rb',
		'scoop/bucket/actionlint.json',
		'aur/actionlint-kjanat-bin.pkgbuild',
		'aur/actionlint-kjanat-bin.srcinfo',
		'aur/actionlint-kjanat.pkgbuild',
		'aur/actionlint-kjanat.srcinfo',
		'actionlint_1.2.3_linux_amd64.tar.gz',
		'actionlint_1.2.3_checksums.txt',
		'actionlint.sbom.json',
		'linux_amd64_v1/actionlint',
	];
	for (const file of files) {
		const path = join(root, 'dist', file);
		await mkdir(dirname(path), { recursive: true });
		await writeFile(path, `${file}\r\nbytes\0`);
	}
	await writeFile(join(output, 'actionlint-action_1.2.3.mjs'), 'bundle');
	await writeFile(
		join(root, 'dist/artifacts.json'),
		JSON.stringify([
			{ type: 'Archive', name: files[6], path: `dist/${files[6]}` },
			{ type: 'Checksum', name: files[7], path: `dist/${files[7]}` },
			{ type: 'SBOM', name: files[8], path: `dist/${files[8]}` },
			{ type: 'Binary', internal_type: 4, name: 'actionlint', path: `dist/${files[9]}` },
			{ type: 'File', name: 'actionlint-action_1.2.3.mjs', path: join(output, 'actionlint-action_1.2.3.mjs') },
		]),
	);
	await collectAssets(root, output);
	assert.equal((await readdir(output)).length, 10);
	assert.deepEqual(await readFile(join(output, files[6])), await readFile(join(root, 'dist', files[6])));
	assert.deepEqual(
		await readFile(join(output, 'actionlint-homebrew.rb')),
		await readFile(join(root, 'dist', files[0])),
	);
	assert.deepEqual(
		await readFile(join(output, 'actionlint-kjanat.srcinfo')),
		await readFile(join(root, 'dist', files[5])),
	);
	assert.ok(!(await readdir(output)).includes('artifacts.json'));
});

test('collection rejects absent outputs and name collisions rather than dropping artifacts', async (t) => {
	const root = await fixture(t);
	await mkdir(join(root, 'dist'));
	await writeFile(join(root, 'dist/artifacts.json'), '[]');
	await assert.rejects(collectAssets(root, join(root, 'assets')), /no release archives/);
	await writeFile(join(root, 'dist/a.zip'), 'archive');
	await writeFile(
		join(root, 'dist/artifacts.json'),
		JSON.stringify([
			{ type: 'Archive', name: 'a.zip', path: 'dist/a.zip' },
			{ type: 'Archive', name: 'a.zip', path: 'dist/a.zip' },
		]),
	);
	await assert.rejects(collectAssets(root, join(root, 'assets')), /EEXIST/);
});

test('publication sends preserved bytes with a concurrency guard; identical reruns do not commit', async (t) => {
	const root = await fixture(t);
	await writeFile(join(root, 'actionlint-homebrew.rb'), 'cask\n');
	await writeFile(join(root, 'actionlint-scoop.json'), '{"version":"1.2.3"}\n');
	const calls = [];
	const request = async (url, options) => {
		calls.push({ url, options });
		if (options.method === 'PUT') return new Response('{}', { status: 200 });
		return Response.json({
			sha: 'previous',
			encoding: 'base64',
			content: Buffer.from(url.includes('scoop') ? '{"version":"1.2.3"}\n' : 'old').toString('base64'),
		});
	};
	await publishManifests(root, '1.2.3', { HOMEBREW_TAP_TOKEN: 'brew-test', SCOOP_BUCKET_TOKEN: 'scoop-test' }, request);
	assert.equal(calls.length, 3);
	const update = calls.find((call) => call.options.method === 'PUT');
	assert.ok(update);
	assert.deepEqual(JSON.parse(update.options.body), {
		message: 'Update actionlint to v1.2.3',
		branch: 'master',
		content: Buffer.from('cask\n').toString('base64'),
		sha: 'previous',
	});
	assert.equal(update.url, 'https://api.github.com/repos/kjanat/homebrew-tap/contents/Casks/actionlint.rb');
});

test('missing permissions fail without silently skipping distribution updates', async (t) => {
	const root = await fixture(t);
	await writeFile(join(root, 'actionlint-homebrew.rb'), 'cask');
	await assert.rejects(publishManifests(root, '1.2.3', {}), /Missing publication token/);
	await assert.rejects(
		publishManifests(
			root,
			'1.2.3',
			{ HOMEBREW_TAP_TOKEN: 'test', SCOOP_BUCKET_TOKEN: 'test' },
			async () => new Response('', { status: 403 }),
		),
		/HTTP 403/,
	);
});
