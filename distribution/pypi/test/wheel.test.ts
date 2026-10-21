import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';

import { execFileSync, type SpawnSyncReturns, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { appendFile, chmod, copyFile, mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { readZip, writeZip } from '#archive';
import { buildWheels, macosPlatform, macosVersion, parseChecksums, targets } from '#wheel';

const version = '1.2.3';
const stub = `#!/bin/sh\necho "actionlint ${version}"\necho "args $*"\necho "pid $$"\nexit "\${STUB_EXIT:-0}"\n`;
const license = 'MIT License\n\nstub\n';
const cli = fileURLToPath(new URL('../src/cli.ts', import.meta.url));
const launcher = await readFile(new URL('../python/actionlint_kjanat/__init__.py', import.meta.url));
const info = `actionlint_kjanat-${version}.dist-info`;

const loadCommand = (command: number, words: readonly number[]) => {
	const buffer = Buffer.alloc(8 + 4 * words.length);
	buffer.writeUInt32LE(command, 0);
	buffer.writeUInt32LE(buffer.length, 4);
	for (const [index, word] of words.entries()) buffer.writeUInt32LE(word, 8 + 4 * index);
	return buffer;
};
const machO = (...commands: Buffer[]) => {
	const header = Buffer.alloc(32);
	header.writeUInt32LE(0xfeedfacf, 0);
	header.writeUInt32LE(0x01000007, 4);
	header.writeUInt32LE(2, 12);
	header.writeUInt32LE(commands.length, 16);
	header.writeUInt32LE(commands.reduce((size, command) => size + command.length, 0), 20);
	return Buffer.concat([header, ...commands]);
};
const macosEncoded = (major: number, minor: number) => (major << 16) | (minor << 8);
const segment = loadCommand(0x19, [0, 0, 0, 0]);
const buildVersionFor = (platform: number, major: number, minor: number) =>
	loadCommand(0x32, [platform, macosEncoded(major, minor), macosEncoded(26, 2), 0]);
const buildVersion = (major: number, minor: number) => buildVersionFor(1, major, minor);
const versionMin = (major: number, minor: number) =>
	loadCommand(0x24, [macosEncoded(major, minor), macosEncoded(26, 2)]);
const binaries: Record<string, Buffer> = {
	darwin_amd64: machO(segment, versionMin(10, 13)),
	darwin_arm64: machO(segment, buildVersion(12, 0)),
};

const expectedWheels = [
	'actionlint_kjanat-1.2.3-py3-none-macosx_10_13_x86_64.whl',
	'actionlint_kjanat-1.2.3-py3-none-macosx_12_0_arm64.whl',
	'actionlint_kjanat-1.2.3-py3-none-manylinux_2_17_i686.whl',
	'actionlint_kjanat-1.2.3-py3-none-musllinux_1_1_i686.whl',
	'actionlint_kjanat-1.2.3-py3-none-manylinux_2_17_x86_64.whl',
	'actionlint_kjanat-1.2.3-py3-none-musllinux_1_1_x86_64.whl',
	'actionlint_kjanat-1.2.3-py3-none-manylinux_2_17_aarch64.whl',
	'actionlint_kjanat-1.2.3-py3-none-musllinux_1_1_aarch64.whl',
	'actionlint_kjanat-1.2.3-py3-none-manylinux_2_17_armv7l.whl',
	'actionlint_kjanat-1.2.3-py3-none-musllinux_1_1_armv7l.whl',
	'actionlint_kjanat-1.2.3-py3-none-win32.whl',
	'actionlint_kjanat-1.2.3-py3-none-win_amd64.whl',
	'actionlint_kjanat-1.2.3-py3-none-win_arm64.whl',
];

let root = '';
let out = '';
let built: SpawnSyncReturns<string>;

const sha256 = (data: Buffer) => createHash('sha256').update(data).digest('hex');
const archiveName = (asset: string) => {
	const target = targets.find(candidate => candidate.asset === asset);
	assert.ok(target);
	return `actionlint_${version}_${asset}.${target.format}`;
};
const readWheel = async (platform: string) =>
	readZip(await readFile(join(out, `actionlint_kjanat-${version}-py3-none-${platform}.whl`)));
const text = (files: Map<string, Buffer>, path: string) => {
	const file = files.get(path);
	assert.ok(file, `wheel has no ${path}`);
	return file.toString('utf8');
};

async function writeChecksums(directory: string): Promise<void> {
	const names = (await readdir(directory)).filter(name => /\.(tar\.gz|zip)$/.test(name)).sort();
	const lines = await Promise.all(
		names.map(async name => `${sha256(await readFile(join(directory, name)))}  ${name}\n`),
	);
	await writeFile(join(directory, `actionlint_${version}_checksums.txt`), lines.join(''));
}

async function stage(): Promise<string> {
	const directory = await mkdtemp(join(root, 'assets-'));
	for (const target of targets) {
		const content = join(directory, `content-${target.asset}`);
		await mkdir(join(content, 'docs'), { recursive: true });
		await writeFile(join(content, target.binary), binaries[target.asset] ?? stub, { mode: 0o755 });
		await writeFile(join(content, 'LICENSE.txt'), license);
		const archive = `../${archiveName(target.asset)}`;
		const members = [target.binary, 'LICENSE.txt', 'docs'];
		if (target.format === 'zip') {
			const level = target.asset === 'windows_386' ? '-0' : '-9';
			execFileSync('zip', [level, '-q', '-r', archive, ...members], { cwd: content });
		} else {
			execFileSync('tar', ['--format=ustar', '-czf', archive, ...members], { cwd: content });
		}
	}
	await writeChecksums(directory);
	return directory;
}

before(async () => {
	root = await mkdtemp(join(tmpdir(), 'actionlint-pypi-'));
	out = join(root, 'wheels');
	const assets = await stage();
	built = spawnSync(process.execPath, [cli, 'build', '--version', version, '--assets', assets, '--out', out], {
		encoding: 'utf8',
	});
});

after(async () => {
	await rm(root, { recursive: true, force: true });
});

test('the CLI writes one wheel per platform tag and prints each path', async () => {
	assert.equal(built.stderr, '');
	assert.equal(built.status, 0);
	assert.deepEqual(built.stdout.trimEnd().split('\n'), expectedWheels.map(name => join(out, name)));
	assert.deepEqual((await readdir(out)).sort(), [...expectedWheels].sort());
});

test('a wheel carries the launcher, the archived binary and licence, and its metadata', async () => {
	const files = await readWheel('manylinux_2_17_x86_64');
	assert.deepEqual([...files.keys()], [
		'actionlint_kjanat/__init__.py',
		'actionlint_kjanat/bin/actionlint',
		`${info}/METADATA`,
		`${info}/WHEEL`,
		`${info}/entry_points.txt`,
		`${info}/licenses/LICENSE.txt`,
		`${info}/RECORD`,
	]);
	assert.deepEqual(files.get('actionlint_kjanat/__init__.py'), launcher);
	assert.equal(text(files, 'actionlint_kjanat/bin/actionlint'), stub);
	assert.equal(text(files, `${info}/licenses/LICENSE.txt`), license);
	assert.equal(
		text(files, `${info}/WHEEL`),
		'Wheel-Version: 1.0\nGenerator: actionlint-pypi\nRoot-Is-Purelib: false\nTag: py3-none-manylinux_2_17_x86_64\n',
	);
	assert.equal(
		text(files, `${info}/entry_points.txt`),
		'[console_scripts]\nactionlint = actionlint_kjanat:main\n',
	);
	const [headers = '', description = ''] = text(files, `${info}/METADATA`).split(/\n\n(.*)/s);
	assert.deepEqual(headers.split('\n'), [
		'Metadata-Version: 2.4',
		'Name: actionlint-kjanat',
		'Version: 1.2.3',
		'Summary: Static checker for GitHub Actions workflow files',
		'Keywords: actionlint,github-actions,workflow,linter,lint,static-analysis,cli,yaml,shellcheck',
		'Author-email: Kaj Kowalski <info+actionlint@kajkowalski.nl>',
		'License-Expression: MIT',
		'License-File: LICENSE.txt',
		'Project-URL: Homepage, https://actionlint.kjanat.dev',
		'Project-URL: Documentation, https://github.com/kjanat/actionlint/tree/v1.2.3/docs',
		'Project-URL: Repository, https://github.com/kjanat/actionlint',
		'Project-URL: Issues, https://github.com/kjanat/actionlint/issues',
		'Description-Content-Type: text/markdown',
	]);
	assert.match(description, /^# actionlint-kjanat\n/);
	assert.match(description, /\npip install actionlint-kjanat\n/);
	assert.match(description, /\(https:\/\/github\.com\/kjanat\/actionlint\/releases\/tag\/v1\.2\.3\)/);
});

test('RECORD lists the hash and size of every other file in the wheel', async () => {
	for (const name of expectedWheels) {
		const files = readZip(await readFile(join(out, name)));
		const lines = text(files, `${info}/RECORD`).trimEnd().split('\n');
		assert.equal(lines.at(-1), `${info}/RECORD,,`);
		const listed = lines.slice(0, -1).map(line => line.split(','));
		assert.deepEqual(listed.map(([path]) => path), [...files.keys()].filter(path => path !== `${info}/RECORD`));
		for (const [path = '', hash, size] of listed) {
			const data = files.get(path);
			assert.ok(data, `${name} has no ${path}`);
			assert.equal(hash, `sha256=${createHash('sha256').update(data).digest('base64url')}`);
			assert.equal(size, String(data.length));
		}
	}
});

test('Windows wheels carry actionlint.exe and Linux tags share one payload', async () => {
	for (const platform of ['win32', 'win_amd64', 'win_arm64']) {
		const files = await readWheel(platform);
		assert.equal(text(files, 'actionlint_kjanat/bin/actionlint.exe'), stub);
		assert.equal(files.has('actionlint_kjanat/bin/actionlint'), false);
		assert.match(text(files, `${info}/WHEEL`), new RegExp(`\nTag: py3-none-${platform}\n`));
	}
	for (const arch of ['i686', 'x86_64', 'aarch64', 'armv7l']) {
		const glibc = await readWheel(`manylinux_2_17_${arch}`);
		const musl = await readWheel(`musllinux_1_1_${arch}`);
		for (const path of ['actionlint_kjanat/__init__.py', 'actionlint_kjanat/bin/actionlint', `${info}/METADATA`]) {
			assert.deepEqual(glibc.get(path), musl.get(path));
		}
	}
});

test('the installed console script execs the bundled binary and the Windows branch propagates its exit code', async () => {
	const venv = join(root, 'venv');
	execFileSync('python3', ['-m', 'venv', venv]);
	const scripts = join(venv, process.platform === 'win32' ? 'Scripts' : 'bin');
	const python = join(scripts, process.platform === 'win32' ? 'python.exe' : 'python');
	const hostPlatform = new Map([
		['win32-ia32', 'win32'],
		['win32-x64', 'win_amd64'],
		['win32-arm64', 'win_arm64'],
		['darwin-x64', 'macosx_10_13_x86_64'],
		['darwin-arm64', 'macosx_12_0_arm64'],
		['linux-ia32', 'manylinux_2_17_i686'],
		['linux-x64', 'manylinux_2_17_x86_64'],
		['linux-arm64', 'manylinux_2_17_aarch64'],
		['linux-arm', 'manylinux_2_17_armv7l'],
	]).get(`${process.platform}-${process.arch}`);
	assert.ok(hostPlatform, `unsupported test host ${process.platform}-${process.arch}`);
	const wheel = join(out, `actionlint_kjanat-${version}-py3-none-${hostPlatform}.whl`);
	execFileSync(python, ['-m', 'pip', 'install', '--quiet', '--no-index', '--no-deps', wheel]);
	const location = 'import os, actionlint_kjanat; print(os.path.dirname(actionlint_kjanat.__file__))';
	const bin = join(execFileSync(python, ['-c', location], { encoding: 'utf8' }).trim(), 'bin');
	const executable = join(bin, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');
	if (process.platform === 'win32') {
		const source = join(root, 'stub.go');
		await writeFile(
			source,
			`package main
import ("fmt"; "os"; "strconv"; "strings")
func main() {
 fmt.Printf("actionlint ${version}\\nargs %s\\npid %d\\n", strings.Join(os.Args[1:], " "), os.Getpid())
 code, _ := strconv.Atoi(os.Getenv("STUB_EXIT"))
 os.Exit(code)
}
`,
		);
		await rm(executable);
		execFileSync('go', ['build', '-o', executable, source], { env: { ...process.env, CGO_ENABLED: '0' } });
	} else {
		await writeFile(executable, stub, { mode: 0o755 });
		await chmod(executable, 0o755);
	}
	const command = join(scripts, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');

	const ok = spawnSync(command, ['-version'], { encoding: 'utf8' });
	assert.equal(ok.stderr, '');
	assert.equal(ok.status, 0);
	assert.match(ok.stdout, /^actionlint 1\.2\.3\nargs -version\npid (\d+)\n$/);
	if (process.platform === 'win32') {
		assert.notEqual(ok.stdout, `actionlint 1.2.3\nargs -version\npid ${ok.pid}\n`);
	} else {
		assert.equal(ok.stdout, `actionlint 1.2.3\nargs -version\npid ${ok.pid}\n`);
	}

	const failing = spawnSync(command, ['check', 'a.yml'], {
		encoding: 'utf8',
		env: { ...process.env, STUB_EXIT: '3' },
	});
	assert.equal(failing.status, 3);
	assert.match(failing.stdout, /^actionlint 1\.2\.3\nargs check a.yml\npid (\d+)\n$/);
	if (process.platform !== 'win32') {
		assert.equal(failing.stdout, `actionlint 1.2.3\nargs check a.yml\npid ${failing.pid}\n`);
	}

	if (process.platform !== 'win32') {
		await copyFile(executable, join(bin, 'actionlint.exe'));
		await chmod(join(bin, 'actionlint.exe'), 0o755);
	}
	const windows = spawnSync(
		python,
		[
			'-c',
			"import os, sys, actionlint_kjanat\nos.name = 'nt'\nsys.argv = ['actionlint', '-version']\nactionlint_kjanat.main()",
		],
		{ encoding: 'utf8', env: { ...process.env, STUB_EXIT: '5' } },
	);
	assert.equal(windows.stderr, '');
	assert.equal(windows.status, 5);
	assert.match(windows.stdout, /^actionlint 1\.2\.3\nargs -version\npid (\d+)\n$/);
	assert.notEqual(windows.stdout, `actionlint 1.2.3\nargs -version\npid ${windows.pid}\n`);
});

test('the build command requires every flag', () => {
	const result = spawnSync(process.execPath, [cli, 'build', '--version', version], { encoding: 'utf8' });
	assert.equal(result.stdout, '');
	assert.equal(result.status, 2);
	assert.match(
		result.stderr,
		/\n {2}- flag --assets: Missing required flag --assets\n {2}- flag --out: Missing required flag --out\n/,
	);
});

test('the build command rejects a version outside MAJOR.MINOR.PATCH', () => {
	const result = spawnSync(process.execPath, [cli, 'build', '--version', '1.2', '--assets', root, '--out', out], {
		encoding: 'utf8',
	});
	assert.equal(result.stdout, '');
	assert.equal(result.status, 2);
	assert.match(result.stderr, /^Invalid value '1\.2' for flag --version: must match /);
});

test('buildWheels rejects a version outside MAJOR.MINOR.PATCH', async () => {
	await assert.rejects(buildWheels('1.2', root), /Expected a stable MAJOR\.MINOR\.PATCH version, got 1\.2/);
});

test('buildWheels rejects an archive the checksums do not list', async () => {
	const assets = await stage();
	const checksums = join(assets, `actionlint_${version}_checksums.txt`);
	const lines = (await readFile(checksums, 'utf8')).split('\n').filter(line => !line.endsWith('_linux_arm64.tar.gz'));
	await writeFile(checksums, lines.join('\n'));
	await assert.rejects(
		buildWheels(version, assets),
		/^Error: Checksums list no actionlint_1\.2\.3_linux_arm64\.tar\.gz$/,
	);
});

test('buildWheels rejects an archive whose bytes differ from its checksum', async () => {
	const assets = await stage();
	await appendFile(join(assets, archiveName('windows_arm64')), 'x');
	await assert.rejects(buildWheels(version, assets), /^Error: actionlint_1\.2\.3_windows_arm64\.zip has SHA-256 /);
});

test('buildWheels rejects an archive without the binary', async () => {
	const assets = await stage();
	execFileSync('tar', ['--format=ustar', '-czf', `../${archiveName('linux_amd64')}`, 'LICENSE.txt'], {
		cwd: join(assets, 'content-linux_amd64'),
	});
	await writeChecksums(assets);
	await assert.rejects(
		buildWheels(version, assets),
		/^Error: actionlint_1\.2\.3_linux_amd64\.tar\.gz has no actionlint$/,
	);
});

test('parseChecksums rejects a malformed line', () => {
	assert.throws(() => parseChecksums('\nnot a checksum\n'), /^Error: Malformed checksum line 2: not a checksum$/);
});

test('readZip rejects data without an end of central directory record', () => {
	assert.throws(() => readZip(Buffer.from('not a zip')), /^Error: Zip archive has no end of central directory record$/);
});

test('readZip rejects an unsupported compression method', () => {
	const archive = writeZip([{ path: 'a.txt', data: Buffer.from('a'), mode: 0o644 }]);
	archive.writeUInt16LE(12, archive.indexOf(Buffer.from([0x50, 0x4b, 0x01, 0x02])) + 10);
	assert.throws(() => readZip(archive), /^Error: Zip entry a\.txt uses unsupported compression method 12$/);
});

test('macosPlatform tags a wheel with the minimum macOS version of the binary', () => {
	assert.equal(macosPlatform(machO(segment, buildVersion(13, 0)), 'b', 'arm64'), 'macosx_13_0_arm64');
	assert.equal(macosPlatform(machO(versionMin(10, 12)), 'b', 'x86_64'), 'macosx_10_12_x86_64');
	assert.deepEqual(macosVersion(machO(buildVersion(14, 2)), 'b'), { major: 14, minor: 2 });
});

test('the darwin wheels carry the tag their binary declares', async () => {
	assert.deepEqual(
		(await readWheel('macosx_10_13_x86_64')).get('actionlint_kjanat/bin/actionlint'),
		binaries['darwin_amd64'],
	);
	assert.deepEqual(
		(await readWheel('macosx_12_0_arm64')).get('actionlint_kjanat/bin/actionlint'),
		binaries['darwin_arm64'],
	);
});

test('macosVersion rejects a darwin binary without LC_BUILD_VERSION or LC_VERSION_MIN_MACOSX', () => {
	assert.throws(
		() => macosVersion(machO(segment), 'a.tar.gz/actionlint'),
		/^Error: a\.tar\.gz\/actionlint has neither LC_BUILD_VERSION nor LC_VERSION_MIN_MACOSX$/,
	);
});

test('macosVersion rejects data that is not a 64-bit Mach-O executable', () => {
	for (const data of [Buffer.from(stub), Buffer.alloc(2)]) {
		assert.throws(() => macosVersion(data, 'b'), /^Error: b is not a 64-bit Mach-O executable$/);
	}
});

test('macosPlatform rejects a macOS 11 or later minimum with a nonzero minor version', () => {
	assert.throws(
		() => macosPlatform(machO(buildVersion(13, 3)), 'b', 'arm64'),
		/^Error: b requires macOS 13\.3, and pip matches macOS 11 and later only as MAJOR_0$/,
	);
});

test('macosVersion skips LC_BUILD_VERSION for another platform and reads the macOS one', () => {
	assert.deepEqual(macosVersion(machO(buildVersionFor(2, 17, 0), buildVersionFor(1, 13, 0)), 'b'), {
		major: 13,
		minor: 0,
	});
});

test('macosVersion rejects a binary whose only LC_BUILD_VERSION names another platform', () => {
	assert.throws(
		() => macosVersion(machO(segment, buildVersionFor(2, 17, 0)), 'b'),
		/^Error: b has LC_BUILD_VERSION only for platform 2, and macOS is platform 1$/,
	);
});

test('macosVersion prefers a macOS LC_BUILD_VERSION and falls back to LC_VERSION_MIN_MACOSX', () => {
	assert.deepEqual(macosVersion(machO(versionMin(10, 12), buildVersion(13, 0)), 'b'), { major: 13, minor: 0 });
	assert.deepEqual(macosVersion(machO(buildVersionFor(2, 17, 0), versionMin(10, 13)), 'b'), { major: 10, minor: 13 });
});
