import pyflakes from '#tools/pyflakes' with { type: 'json' };
import shellcheck from '#tools/shellcheck' with { type: 'json' };

export type RunnerPlatform = {
	os: 'linux' | 'darwin' | 'windows';
	arch: 'amd64' | 'arm64';
};

export type ReleaseAsset = {
	name: string;
	url: string;
	sha256: string;
	archive: 'tar.gz' | 'zip';
};

export function runnerPlatform(os: string, arch: string): RunnerPlatform {
	if (os !== 'linux' && os !== 'darwin' && os !== 'win32') {
		throw new Error(`Unsupported runner operating system: ${os}`);
	}
	if (arch !== 'x64' && arch !== 'arm64') {
		throw new Error(`Unsupported runner architecture: ${arch}`);
	}
	return { os: os === 'win32' ? 'windows' : os, arch: arch === 'x64' ? 'amd64' : 'arm64' };
}

export function nativeAssetName(version: string, platform: RunnerPlatform): string {
	return `actionlint_${version}_${platform.os}_${platform.arch}.${platform.os === 'windows' ? 'zip' : 'tar.gz'}`;
}

export function checksumForAsset(checksums: string, filename: string): string {
	let digest: string | undefined;
	for (const line of checksums.split(/\r?\n/)) {
		const match = /^([a-fA-F0-9]{64}) [ *](.+)$/.exec(line);
		if (match?.[2] !== filename) continue;
		if (digest !== undefined) throw new Error(`Duplicate checksum for ${filename}`);
		digest = match[1]?.toLowerCase();
	}
	if (digest === undefined) throw new Error(`Missing SHA-256 checksum for ${filename}`);
	return digest;
}

export const shellcheckVersion = shellcheck.tagName.replace(/^v/, '');

type ShellcheckRelease = {
	tagName: string;
	assets: readonly { name: string; url: string; digest: string | null }[];
};

export function shellcheckAsset(platform: RunnerPlatform, release: ShellcheckRelease = shellcheck): ReleaseAsset {
	const arch = platform.arch === 'amd64' ? 'x86_64' : 'aarch64';
	// ShellCheck's Windows binary also runs under x64 emulation on ARM64.
	const target = platform.os === 'windows' ? 'zip' : `${platform.os}.${arch}.tar.gz`;
	const name = `shellcheck-${release.tagName}.${target}`;
	const asset = release.assets.find((candidate) => candidate.name === name);
	if (!asset) throw new Error(`Missing ShellCheck release asset: ${name}`);
	const sha256 = /^sha256:([a-fA-F0-9]{64})$/.exec(asset.digest ?? '')?.[1];
	if (!sha256) throw new Error(`Missing or invalid SHA-256 digest for ShellCheck asset: ${name}`);
	return {
		name: asset.name,
		url: asset.url,
		sha256: sha256.toLowerCase(),
		archive: platform.os === 'windows' ? 'zip' : 'tar.gz',
	};
}

export const pyflakesVersion = pyflakes.info.version;
const wheel = pyflakes.urls.find((asset) =>
	asset.packagetype === 'bdist_wheel' && asset.filename.endsWith('-none-any.whl')
);
if (!wheel) throw new Error(`Missing universal pyflakes wheel for ${pyflakesVersion}`);
export const pyflakesAsset: ReleaseAsset = {
	name: wheel.filename,
	url: wheel.url,
	sha256: wheel.digests.sha256,
	archive: 'zip',
};
