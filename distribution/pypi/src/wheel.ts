import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';

import { readTarGz, readZip, writeZip, type ZipEntry } from '#archive';

export const distribution = 'actionlint-kjanat';
export const importName = 'actionlint_kjanat';
export const repositoryUrl = 'https://github.com/kjanat/actionlint';

const posix = { format: 'tar.gz', binary: 'actionlint' } as const;
const windows = { format: 'zip', binary: 'actionlint.exe' } as const;

export type Platforms =
	| { kind: 'tags'; tags: readonly string[] }
	| { kind: 'macos'; arch: 'x86_64' | 'arm64' };

export interface Target {
	asset: string;
	format: 'tar.gz' | 'zip';
	binary: 'actionlint' | 'actionlint.exe';
	platforms: Platforms;
}

const tags = (...list: string[]) => ({ kind: 'tags', tags: list }) as const;
const macos = (arch: 'x86_64' | 'arm64') => ({ kind: 'macos', arch }) as const;

export const targets = [
	{ asset: 'darwin_amd64', ...posix, platforms: macos('x86_64') },
	{ asset: 'darwin_arm64', ...posix, platforms: macos('arm64') },
	{ asset: 'linux_386', ...posix, platforms: tags('manylinux_2_17_i686', 'musllinux_1_1_i686') },
	{ asset: 'linux_amd64', ...posix, platforms: tags('manylinux_2_17_x86_64', 'musllinux_1_1_x86_64') },
	{ asset: 'linux_arm64', ...posix, platforms: tags('manylinux_2_17_aarch64', 'musllinux_1_1_aarch64') },
	{ asset: 'linux_armv6', ...posix, platforms: tags('manylinux_2_17_armv7l', 'musllinux_1_1_armv7l') },
	{ asset: 'windows_386', ...windows, platforms: tags('win32') },
	{ asset: 'windows_amd64', ...windows, platforms: tags('win_amd64') },
	{ asset: 'windows_arm64', ...windows, platforms: tags('win_arm64') },
] as const satisfies readonly Target[];

const machO64Magic = 0xfeedfacf;
const lcBuildVersion = 0x32;
const lcVersionMinMacosx = 0x24;
const platformMacos = 1;

export function macosVersion(binary: Buffer, name: string): { major: number; minor: number } {
	if (binary.length < 32 || binary.readUInt32LE(0) !== machO64Magic) {
		throw new Error(`${name} is not a 64-bit Mach-O executable`);
	}
	const decode = (version: number) => ({ major: version >>> 16, minor: (version >>> 8) & 0xff });
	const otherPlatforms: number[] = [];
	let versionMin: number | undefined;
	let offset = 32;
	for (let index = 0; index < binary.readUInt32LE(16); index++) {
		const command = binary.readUInt32LE(offset);
		if (command === lcVersionMinMacosx) versionMin = binary.readUInt32LE(offset + 8);
		if (command === lcBuildVersion) {
			const platform = binary.readUInt32LE(offset + 8);
			if (platform === platformMacos) return decode(binary.readUInt32LE(offset + 12));
			otherPlatforms.push(platform);
		}
		offset += binary.readUInt32LE(offset + 4);
	}
	if (versionMin !== undefined) return decode(versionMin);
	if (otherPlatforms.length > 0) {
		throw new Error(
			`${name} has LC_BUILD_VERSION only for platform ${otherPlatforms.join(', ')}, and macOS is platform 1`,
		);
	}
	throw new Error(`${name} has neither LC_BUILD_VERSION nor LC_VERSION_MIN_MACOSX`);
}

export function macosPlatform(binary: Buffer, name: string, arch: 'x86_64' | 'arm64'): string {
	const { major, minor } = macosVersion(binary, name);
	if (major >= 11 && minor !== 0) {
		throw new Error(`${name} requires macOS ${major}.${minor}, and pip matches macOS 11 and later only as MAJOR_0`);
	}
	return `macosx_${major}_${minor}_${arch}`;
}

export interface Wheel {
	name: string;
	data: Buffer;
}

export function parseChecksums(text: string): Map<string, string> {
	const checksums = new Map<string, string>();
	for (const [index, line] of text.split(/\r?\n/).entries()) {
		if (line === '') continue;
		const match = /^([0-9a-f]{64}) {2}(\S+)$/.exec(line);
		if (!match?.[1] || !match[2]) throw new Error(`Malformed checksum line ${index + 1}: ${line}`);
		checksums.set(match[2], match[1]);
	}
	return checksums;
}

export function metadata(version: string): string {
	const tag = `v${version}`;
	return [
		'Metadata-Version: 2.4',
		`Name: ${distribution}`,
		`Version: ${version}`,
		'Summary: Static checker for GitHub Actions workflow files',
		'Keywords: actionlint,github-actions,workflow,linter,lint,static-analysis,cli,yaml,shellcheck',
		'Author-email: Kaj Kowalski <info+actionlint@kajkowalski.nl>',
		'License-Expression: MIT',
		'License-File: LICENSE.txt',
		'Project-URL: Homepage, https://actionlint.kjanat.dev',
		`Project-URL: Documentation, ${repositoryUrl}/tree/${tag}/docs`,
		`Project-URL: Repository, ${repositoryUrl}`,
		`Project-URL: Issues, ${repositoryUrl}/issues`,
		'Description-Content-Type: text/markdown',
		'',
		`# ${distribution}`,
		'',
		'[actionlint](https://actionlint.kjanat.dev) checks GitHub Actions workflow files for syntax errors, type errors in',
		'expressions, unsafe script injection and many other mistakes. This package installs the prebuilt binary of the',
		`[kjanat/actionlint](${repositoryUrl}) fork as the \`actionlint\` command.`,
		'',
		'```sh',
		`pip install ${distribution}`,
		'actionlint',
		'```',
		'',
		`Each wheel carries the binary from the [${tag} release](${repositoryUrl}/releases/tag/${tag}) for its platform.`,
		'ShellCheck and Ruff are optional. Install them on `PATH` to check shell and Python scripts in `run:` steps.',
		'',
	].join('\n');
}

function record(entries: readonly ZipEntry[], path: string): string {
	const lines = entries.map(entry => {
		const digest = createHash('sha256').update(entry.data).digest('base64url');
		return `${entry.path},sha256=${digest},${entry.data.length}`;
	});
	return [...lines, `${path},,`, ''].join('\n');
}

export function wheel(
	version: string,
	platform: string,
	target: Target,
	files: { binary: Buffer; license: Buffer; launcher: Buffer },
): Wheel {
	const info = `${importName}-${version}.dist-info`;
	const text = (data: string) => Buffer.from(data, 'utf8');
	const entries: ZipEntry[] = [
		{ path: `${importName}/__init__.py`, data: files.launcher, mode: 0o644 },
		{ path: `${importName}/bin/${target.binary}`, data: files.binary, mode: 0o755 },
		{ path: `${info}/METADATA`, data: text(metadata(version)), mode: 0o644 },
		{
			path: `${info}/WHEEL`,
			data: text(`Wheel-Version: 1.0\nGenerator: actionlint-pypi\nRoot-Is-Purelib: false\nTag: py3-none-${platform}\n`),
			mode: 0o644,
		},
		{
			path: `${info}/entry_points.txt`,
			data: text(`[console_scripts]\nactionlint = ${importName}:main\n`),
			mode: 0o644,
		},
		{ path: `${info}/licenses/LICENSE.txt`, data: files.license, mode: 0o644 },
	];
	const recordPath = `${info}/RECORD`;
	entries.push({ path: recordPath, data: text(record(entries, recordPath)), mode: 0o644 });
	return { name: `${importName}-${version}-py3-none-${platform}.whl`, data: writeZip(entries) };
}

function pick(files: Map<string, Buffer>, archive: string, name: string): Buffer {
	const file = files.get(name);
	if (!file) throw new Error(`${archive} has no ${name}`);
	return file;
}

export async function buildWheels(version: string, assets: string): Promise<Wheel[]> {
	if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
		throw new Error(`Expected a stable MAJOR.MINOR.PATCH version, got ${version}`);
	}
	const checksums = parseChecksums(await readFile(join(assets, `actionlint_${version}_checksums.txt`), 'utf8'));
	const launcher = await readFile(new URL(`../python/${importName}/__init__.py`, import.meta.url));
	const wheels: Wheel[] = [];
	for (const target of targets) {
		const archive = `actionlint_${version}_${target.asset}.${target.format}`;
		const expected = checksums.get(archive);
		if (!expected) throw new Error(`Checksums list no ${archive}`);
		const bytes = await readFile(join(assets, archive));
		const actual = createHash('sha256').update(bytes).digest('hex');
		if (actual !== expected) throw new Error(`${archive} has SHA-256 ${actual}, checksums list ${expected}`);
		const files = target.format === 'zip' ? readZip(bytes) : readTarGz(bytes);
		const contents = {
			binary: pick(files, archive, target.binary),
			license: pick(files, archive, 'LICENSE.txt'),
			launcher,
		};
		const platforms = target.platforms.kind === 'macos'
			? [macosPlatform(contents.binary, `${archive}/${target.binary}`, target.platforms.arch)]
			: target.platforms.tags;
		for (const platform of platforms) wheels.push(wheel(version, platform, target, contents));
	}
	return wheels;
}
