import assert from 'node:assert/strict';
import type { TestContext } from 'node:test';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { cp, mkdir, mkdtemp, readdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';

import { Uint8ArrayReader, Uint8ArrayWriter, ZipWriter } from '@zip.js/zip.js';
import { pack } from 'tar-stream';

import { archiveMembers, readBounded } from '#archives';
import type { BuildOptions } from '#build';
import { assetName, buildPackages, parseChecksums, releaseVersion } from '#build';
import type { ArchiveFormat, Target } from '#targets';
import { jsonObject, loadTargets, packageName, parseTargets, selectTargets } from '#targets';

const sourceRoot = resolve(import.meta.dirname, '../../..');
const npmSources = join(sourceRoot, 'distribution/npm');

async function archive(format: ArchiveFormat, files: ReadonlyMap<string, Buffer>): Promise<Buffer> {
	if (format === 'zip') {
		const writer = new ZipWriter(new Uint8ArrayWriter(), { useWebWorkers: false });
		for (const [name, bytes] of files) await writer.add(name, new Uint8ArrayReader(bytes), { level: 0 });
		return Buffer.from(await writer.close());
	}
	const tar = pack();
	for (const [name, bytes] of files) tar.entry({ name, mode: 0o755 }, bytes);
	tar.finalize();
	return gzipSync(await readBounded(tar, 'tar fixture'));
}

async function fixture(t: TestContext, version = '3.2.1') {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-npm-'));
	t.after(() => rm(root, { recursive: true, force: true }));
	const npmDir = join(root, 'distribution/npm');
	await mkdir(join(npmDir, 'facade'), { recursive: true });
	for (const part of ['bin', 'lib', 'types', 'README.md', 'package.json']) {
		await cp(join(npmSources, 'facade', part), join(npmDir, 'facade', part), { recursive: true });
	}
	await cp(join(npmSources, 'targets.json'), join(npmDir, 'targets.json'));
	for (const name of ['LICENSE.txt', 'actionlint.schema.json', 'schemas']) {
		await cp(join(sourceRoot, name), join(root, name), { recursive: true });
	}
	const options: BuildOptions = {
		version,
		repository: '',
		token: '',
		repoRoot: root,
		npmDir,
		outDir: join(root, 'dist'),
		downloads: join(root, 'downloads'),
		only: '',
	};
	await mkdir(options.downloads);
	const targets = await loadTargets(join(npmDir, 'targets.json'));
	const assets = new Map<string, Buffer>();
	for (const target of targets.targets) {
		const binary = targets.binary + (target.os === 'win32' ? '.exe' : '');
		const bytes = await archive(
			target.format,
			new Map([[binary, Buffer.from(`binary for ${target.pkg}`)], ['man/actionlint.1', Buffer.from('manual')], [
				'README.md',
				Buffer.from('release readme'),
			]]),
		);
		const name = assetName(options.version, target);
		assets.set(name, bytes);
		await writeFile(join(options.downloads, name), bytes);
	}
	const checksums = () =>
		[...assets].map(([name, bytes]) => `${createHash('sha256').update(bytes).digest('hex')}  ${name}\n`).join('');
	await writeFile(join(options.downloads, `actionlint_${options.version}_checksums.txt`), checksums());
	const replaceArchive = async (target: Target, bytes: Buffer) => {
		const name = assetName(options.version, target);
		assets.set(name, bytes);
		await writeFile(join(options.downloads, name), bytes);
		await writeFile(join(options.downloads, `actionlint_${options.version}_checksums.txt`), checksums());
	};
	return { root, options, targets, assets, replaceArchive };
}

async function manifest(path: string): Promise<Record<string, unknown>> {
	const value: unknown = JSON.parse(await readFile(path, 'utf8'));
	return jsonObject(value, 'package.json');
}

test('preview packages include only essential platforms and their archived manual', async t => {
	const version = '2.0.0-SNAPSHOT-9981ea76';
	const f = await fixture(t, version);
	const configured = await manifest(join(f.options.npmDir, 'targets.json'));
	assert.ok(Array.isArray(configured.targets));
	const preview = {
		...configured,
		targets: configured.targets.map(value => jsonObject(value, 'target')).filter(target => target.tier === 1),
	};
	const essential = parseTargets(preview).targets;
	await writeFile(join(f.options.npmDir, 'targets.json'), JSON.stringify(preview));
	const result = await buildPackages(f.options, { report: () => {} });
	assert.deepEqual(result.map(path => path.slice(f.options.outDir.length + 1)).sort(), [
		'darwin-arm64',
		'darwin-x64',
		'facade',
		'linux-arm64',
		'linux-x64',
		'win32-arm64',
		'win32-x64',
	]);
	const facade = await manifest(join(f.options.outDir, 'facade/package.json'));
	assert.equal(facade.version, version);
	assert.deepEqual(
		Object.values(jsonObject(facade.optionalDependencies, 'optionalDependencies')),
		essential.map(() => version),
	);
	assert.deepEqual(
		Object.keys(jsonObject(facade.optionalDependencies, 'optionalDependencies')),
		essential.map(target => packageName(f.targets, target)),
	);
	for (const target of essential) {
		const binary = f.targets.binary + (target.os === 'win32' ? '.exe' : '');
		assert.equal(await readFile(join(f.options.outDir, target.pkg, 'bin', binary), 'utf8'), `binary for ${target.pkg}`);
	}
	assert.equal(await readFile(join(f.options.outDir, 'facade/man/actionlint.1'), 'utf8'), 'manual');
});

test('all platform packages contain their own verified binary and matching npm metadata', async t => {
	const f = await fixture(t);
	const result = await buildPackages(f.options, {
		report: () => {},
		fetch: async () => {
			throw new Error('Unexpected network request');
		},
	});
	assert.equal(result.length, f.targets.targets.length + 1);
	for (const target of f.targets.targets) {
		const directory = join(f.options.outDir, target.pkg);
		const binary = f.targets.binary + (target.os === 'win32' ? '.exe' : '');
		const path = join(directory, 'bin', binary);
		assert.equal(await readFile(path, 'utf8'), `binary for ${target.pkg}`);
		if (process.platform !== 'win32') assert.ok((await stat(path)).mode & 0o111);
		const pkg = await manifest(join(directory, 'package.json'));
		assert.equal(pkg.name, packageName(f.targets, target));
		assert.equal(pkg.version, f.options.version);
		assert.deepEqual(pkg.os, [target.os]);
		assert.deepEqual(pkg.cpu, [target.cpu]);
		assert.deepEqual(pkg.exports, { './package.json': './package.json' });
		assert.equal(pkg.preferUnplugged, true);
		assert.equal(jsonObject(pkg.funding, 'funding').url, 'https://github.com/sponsors/kjanat');
		for (const file of ['README.md', 'LICENSE.txt']) assert.ok((await stat(join(directory, file))).size);
	}
	const facade = join(f.options.outDir, 'facade');
	const pkg = await manifest(join(facade, 'package.json'));
	assert.equal(pkg.version, f.options.version);
	const optional = jsonObject(pkg.optionalDependencies, 'optionalDependencies');
	assert.deepEqual(Object.keys(optional), f.targets.targets.map(target => packageName(f.targets, target)));
	for (const version of Object.values(optional)) assert.equal(version, '3.2.1');
	assert.equal(await readFile(join(facade, 'man/actionlint.1'), 'utf8'), 'manual');
	for (
		const part of [
			'bin/actionlint.mjs',
			'lib/resolve.mjs',
			'lib/launch.mjs',
			'types/result.d.ts',
			'README.md',
			'LICENSE.txt',
			'actionlint.schema.json',
		]
	) {
		const original = ['LICENSE.txt', 'actionlint.schema.json'].includes(part) || part.startsWith('schemas/')
			? join(f.root, part)
			: join(f.options.npmDir, 'facade', part);
		assert.deepEqual(await readFile(join(facade, part)), await readFile(original), part);
	}
	for (const part of await readdir(join(f.root, 'schemas'), { recursive: true })) {
		const path = join(f.root, 'schemas', part);
		if ((await stat(path)).isFile()) {
			assert.deepEqual(await readFile(join(facade, 'schemas', part)), await readFile(path), part);
		}
	}
	const original = await manifest(join(f.options.npmDir, 'facade/package.json'));
	assert.deepEqual(Object.keys(pkg), Object.keys(original));
	assert.equal(original.version, '0.0.0');
});

test('facade-only assembly reads its manual from a verified archive and writes no platform package', async t => {
	const f = await fixture(t);
	await buildPackages({ ...f.options, only: 'facade' }, { report: () => {} });
	assert.deepEqual(await readdir(f.options.outDir), ['facade']);
	const directory = join(f.options.outDir, 'facade');
	assert.equal(await readFile(join(directory, 'man/actionlint.1'), 'utf8'), 'manual');
	assert.deepEqual(
		await readFile(join(directory, 'actionlint.schema.json')),
		await readFile(join(f.root, 'actionlint.schema.json')),
	);
	const pkg = await manifest(join(directory, 'package.json'));
	assert.equal(jsonObject(pkg.exports, 'exports')['./schema'], './actionlint.schema.json');
	assert.equal(jsonObject(pkg.exports, 'exports')['./result.schema.json'], './schemas/results/v1.schema.json');
});

test('facade assembly fails when its configuration schema is missing', async t => {
	const f = await fixture(t);
	await rm(join(f.root, 'actionlint.schema.json'));
	await assert.rejects(
		buildPackages({ ...f.options, only: 'facade' }, { report: () => {} }),
		/actionlint\.schema\.json/,
	);
});

test('a correctly hashed archive without a manual is rejected', async t => {
	const f = await fixture(t);
	const target = f.targets.targets[0];
	assert.ok(target);
	await f.replaceArchive(target, await archive(target.format, new Map([['actionlint', Buffer.from('binary')]])));
	await assert.rejects(buildPackages(f.options, { report: () => {} }), /actionlint\.1/);
});

test('archive checksum mismatches and missing manifest entries stop package creation', async t => {
	const f = await fixture(t);
	const target = f.targets.targets[0];
	assert.ok(target);
	const name = assetName(f.options.version, target);
	await writeFile(join(f.options.downloads, name), 'swapped archive');
	await assert.rejects(buildPackages({ ...f.options, only: target.pkg }, { report: () => {} }), /digest mismatch/);
	assert.deepEqual(await readdir(f.options.outDir), []);
	await writeFile(
		join(f.options.downloads, `actionlint_${f.options.version}_checksums.txt`),
		`${'a'.repeat(64)}  another.tar.gz\n`,
	);
	await assert.rejects(buildPackages({ ...f.options, only: target.pkg }, { report: () => {} }), /no digest/);
});

test('target names, scopes, formats and selection match the launcher naming contract', async () => {
	const targets = await loadTargets(join(npmSources, 'targets.json'));
	const target = targets.targets[0];
	assert.ok(target);
	assert.equal(selectTargets(targets, '').length, targets.targets.length);
	assert.deepEqual(selectTargets(targets, 'facade'), []);
	assert.deepEqual(selectTargets(targets, target.pkg), [target]);
	assert.deepEqual(selectTargets(targets, packageName(targets, target)), [target]);
	assert.throws(() => selectTargets(targets, 'not-a-target'), /No npm target matches/);
	assert.throws(() => parseTargets({ ...targets, targets: [{ ...target, pkg: 'other-name' }] }), /must be named/);
	for (const scope of ['plain', '@bad/scope']) {
		assert.throws(() => parseTargets({ ...targets, platformScope: scope }), /platformScope/);
	}
	assert.throws(
		() => parseTargets({ ...targets, targets: [{ ...target, format: 'rar' }] }),
		/unsupported archive format/,
	);
	assert.throws(() => parseTargets({ ...targets, targets: [target, target] }), /declared twice/);
	assert.equal(releaseVersion('v3.2.1-rc.1'), '3.2.1-rc.1');
	assert.throws(() => releaseVersion('../version'), /not X.Y.Z/);
	assert.throws(() => parseChecksums('not a checksum'), /Malformed/);
	assert.throws(() => parseChecksums(`${'a'.repeat(64)}  file\n${'a'.repeat(64)}  file\n`), /twice/);
});

test('tar and zip readers select regular members by basename and reject ambiguous or corrupt archives', async () => {
	const formats: readonly ArchiveFormat[] = ['tar.gz', 'zip'];
	for (const format of formats) {
		const bytes = await archive(format, new Map([['nested/actionlint', Buffer.from('binary')]]));
		assert.equal((await archiveMembers(bytes, format, ['actionlint'])).get('actionlint')?.toString(), 'binary');
		await assert.rejects(archiveMembers(bytes, format, ['missing']), /No missing/);
		const ambiguous = await archive(
			format,
			new Map([['a/actionlint', Buffer.from('first')], ['b/actionlint', Buffer.from('second')]]),
		);
		await assert.rejects(archiveMembers(ambiguous, format, ['actionlint']), /more than one/);
	}
	const zip = await archive('zip', new Map([['actionlint', Buffer.from('unique binary payload')]]));
	const position = zip.indexOf('unique binary payload');
	assert.ok(position >= 0);
	zip.writeUInt8(zip.readUInt8(position) ^ 0xff, position);
	await assert.rejects(archiveMembers(zip, 'zip', ['actionlint']), /CRC|checksum/i);
	await assert.rejects(archiveMembers(Buffer.from('not gzip'), 'tar.gz', ['actionlint']));
});

test('missing local assets can be downloaded with the GitHub token and still require a matching digest', async t => {
	const f = await fixture(t);
	const target = f.targets.targets[0];
	assert.ok(target);
	const name = assetName(f.options.version, target);
	const bytes = f.assets.get(name);
	assert.ok(bytes);
	await rm(join(f.options.downloads, name));
	const calls: string[] = [];
	const request: typeof fetch = async (url, init) => {
		calls.push(String(url));
		assert.equal(new Headers(init?.headers).get('Authorization'), 'Bearer fixture-token');
		return new Response(new Uint8Array(bytes));
	};
	await buildPackages({ ...f.options, only: target.pkg, repository: 'kjanat/actionlint', token: 'fixture-token' }, {
		fetch: request,
		report: () => {},
	});
	assert.deepEqual(calls, [`https://github.com/kjanat/actionlint/releases/download/v3.2.1/${name}`]);
	assert.equal(
		await readFile(join(f.options.outDir, target.pkg, 'bin/actionlint'), 'utf8'),
		`binary for ${target.pkg}`,
	);
});

test('the Node CLI consumes action inputs without requiring Go or Bun at runtime', async t => {
	const f = await fixture(t);
	const target = f.targets.targets[0];
	assert.ok(target);
	const output = execFileSync(process.execPath, [join(npmSources, 'src/cli.ts'), 'build'], {
		cwd: f.root,
		encoding: 'utf8',
		env: {
			...process.env,
			GITHUB_WORKSPACE: f.root,
			INPUT_VERSION: f.options.version,
			INPUT_NPM_DIR: f.options.npmDir,
			INPUT_OUT_DIR: f.options.outDir,
			INPUT_DOWNLOADS: f.options.downloads,
			INPUT_ONLY: target.pkg,
			INPUT_REPOSITORY: '',
			INPUT_TOKEN: '',
		},
	});
	assert.match(output, /built @kjanat-actionlint\/actionlint-/);
	assert.deepEqual(await readdir(f.options.outDir), [target.pkg]);
	const help = execFileSync(process.execPath, [join(npmSources, 'src/cli.ts'), 'build', '--help'], {
		encoding: 'utf8',
	});
	assert.match(help, /Examples:/);
	assert.match(help, /--downloads \.\/assets --only linux-x64/);
});
