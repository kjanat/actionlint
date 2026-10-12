import { stringify } from 'yaml';

import {
	type DefaultLocaleManifest,
	defaultLocaleManifest,
	type InstallerManifest,
	installerManifest,
	type ManifestType,
	manifestTypes,
	schemaVersion,
	type VersionManifest,
	versionManifest,
} from '#schema';

export const packageIdentifier = 'kjanat.actionlint';
export const repositoryUrl = 'https://github.com/kjanat/actionlint';
export const defaultLocale = 'en-US';

const releaseNotesMaxLength = defaultLocaleManifest.shape.ReleaseNotes.unwrap().maxLength;
if (releaseNotesMaxLength === null) throw new Error('ReleaseNotes schema declares no maxLength');
export const releaseNotesLimit = releaseNotesMaxLength;

type Architecture = InstallerManifest['Installers'][number]['Architecture'];

const architectures = [
	['386', 'x86'],
	['amd64', 'x64'],
	['arm64', 'arm64'],
] as const satisfies readonly (readonly [goarch: string, architecture: Architecture])[];

export interface GenerateOptions {
	version: string;
	releaseDate: string;
	checksums: string;
	releaseNotes: string;
}

export interface Manifests {
	version: VersionManifest;
	installer: InstallerManifest;
	defaultLocale: DefaultLocaleManifest;
}

export const manifestFileNames: Record<ManifestType, string> = {
	version: `${packageIdentifier}.yaml`,
	installer: `${packageIdentifier}.installer.yaml`,
	defaultLocale: `${packageIdentifier}.locale.${defaultLocale}.yaml`,
};

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

export function windowsInstallers(version: string, checksums: Map<string, string>): InstallerManifest['Installers'] {
	const tag = `v${version}`;
	return architectures.map(([goarch, architecture]) => {
		const asset = `actionlint_${version}_windows_${goarch}.zip`;
		const sha256 = checksums.get(asset);
		if (!sha256) throw new Error(`Checksums list no ${asset}`);
		return {
			Architecture: architecture,
			InstallerUrl: `${repositoryUrl}/releases/download/${tag}/${asset}`,
			InstallerSha256: sha256.toUpperCase(),
		};
	});
}

export function capReleaseNotes(notes: string, url: string, limit = releaseNotesLimit): string {
	const trimmed = notes.replaceAll('\r\n', '\n').trim();
	if (trimmed.length <= limit) return trimmed;
	const tail = `\n\nFull release notes: ${url}`;
	let cut = trimmed.lastIndexOf('\n\n', limit - tail.length);
	if (cut <= 0) cut = limit - tail.length;
	return trimmed.slice(0, cut).trimEnd() + tail;
}

export function generateManifests(options: GenerateOptions): Manifests {
	const { version, releaseDate } = options;
	if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Expected stable MAJOR.MINOR.PATCH version');
	const tag = `v${version}`;
	const blob = `${repositoryUrl}/blob/${tag}`;
	const releaseNotesUrl = `${repositoryUrl}/releases/tag/${tag}`;

	const versionFile: VersionManifest = {
		PackageIdentifier: packageIdentifier,
		PackageVersion: version,
		DefaultLocale: defaultLocale,
		ManifestType: 'version',
		ManifestVersion: schemaVersion,
	};

	const installer: InstallerManifest = {
		PackageIdentifier: packageIdentifier,
		PackageVersion: version,
		InstallerLocale: defaultLocale,
		Platform: ['Windows.Desktop'],
		MinimumOSVersion: '10.0.0.0',
		InstallerType: 'zip',
		NestedInstallerType: 'portable',
		NestedInstallerFiles: [{ RelativeFilePath: 'actionlint.exe', PortableCommandAlias: 'actionlint' }],
		Scope: 'user',
		InstallerSuccessCodes: [3],
		UpgradeBehavior: 'install',
		Commands: ['actionlint'],
		ReleaseDate: releaseDate,
		InstallationMetadata: {
			Files: [{
				RelativeFilePath: 'actionlint.exe',
				FileType: 'launch',
				InvocationParameter: '-version',
				DisplayName: 'actionlint',
			}],
		},
		Installers: windowsInstallers(version, parseChecksums(options.checksums)),
		ManifestType: 'installer',
		ManifestVersion: schemaVersion,
	};

	const locale: DefaultLocaleManifest = {
		PackageIdentifier: packageIdentifier,
		PackageVersion: version,
		PackageLocale: defaultLocale,
		Publisher: 'kjanat',
		PublisherUrl: 'https://github.com/kjanat',
		PublisherSupportUrl: `${repositoryUrl}/issues`,
		Author: 'Kaj Kowalski',
		PackageName: 'actionlint',
		PackageUrl: 'https://actionlint.kjanat.dev',
		License: 'MIT',
		LicenseUrl: `${blob}/LICENSE.txt`,
		Copyright: 'Copyright (c) 2026 Kaj Kowalski, Copyright (c) 2021 rhysd',
		CopyrightUrl: `${blob}/LICENSE.txt`,
		ShortDescription: 'Static checker for GitHub Actions workflow files',
		Description:
			'actionlint is a static checker for GitHub Actions workflow files. It checks the workflow syntax, expression '
			+ 'placeholders, the shell scripts in run: steps, and many other mistakes that only surface once a workflow '
			+ 'runs. This is the kjanat fork, published separately from rhysd.actionlint.',
		Tags: ['actionlint', 'ci', 'github-actions', 'linter', 'ruff', 'shellcheck', 'static-analysis', 'workflow', 'yaml'],
		ReleaseNotes: capReleaseNotes(options.releaseNotes, releaseNotesUrl),
		ReleaseNotesUrl: releaseNotesUrl,
		InstallationNotes:
			'ShellCheck and Ruff are optional. When installed, actionlint uses them to check shell and Python '
			+ 'scripts in run steps.',
		Documentations: [
			{ DocumentLabel: 'Usage', DocumentUrl: `${blob}/docs/usage.md` },
			{ DocumentLabel: 'Configuration', DocumentUrl: `${blob}/docs/config.md` },
			{ DocumentLabel: 'Checks', DocumentUrl: `${blob}/docs/checks.md` },
			{ DocumentLabel: 'Installation', DocumentUrl: `${blob}/docs/install.md` },
		],
		ManifestType: 'defaultLocale',
		ManifestVersion: schemaVersion,
	};

	return {
		version: versionManifest.parse(versionFile),
		installer: installerManifest.parse(installer),
		defaultLocale: defaultLocaleManifest.parse(locale),
	};
}

export function renderManifest<T extends ManifestType>(type: T, manifest: Manifests[T]): string {
	const header =
		`# yaml-language-server: $schema=https://aka.ms/winget-manifest.${type}.${manifest.ManifestVersion}.schema.json\n\n`;
	return header + stringify(manifest, { indentSeq: false, lineWidth: 0, blockQuote: 'literal' });
}

export function renderManifests(manifests: Manifests): Record<string, string> {
	return Object.fromEntries(
		manifestTypes.map(type => [manifestFileNames[type], renderManifest(type, manifests[type])]),
	);
}
