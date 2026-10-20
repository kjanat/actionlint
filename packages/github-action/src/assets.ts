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
