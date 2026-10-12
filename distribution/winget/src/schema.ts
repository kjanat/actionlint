import * as z from 'zod';

export const manifestTypes = ['version', 'installer', 'defaultLocale'] as const;
export type ManifestType = (typeof manifestTypes)[number];
export const schemaVersion = '1.12.0';

const versionPart = '(0|[1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])';
const unique = (items: unknown[]): boolean => new Set(items.map(item => JSON.stringify(item))).size === items.length;

const packageIdentifier = z.string().regex(
	/^[^.\s\\/:*?"<>|\x01-\x1f]{1,32}(\.[^.\s\\/:*?"<>|\x01-\x1f]{1,32}){1,7}$/,
).max(128).describe('The package unique identifier');
const packageVersion = z.string().regex(/^[^\\/:*?"<>|\x01-\x1f]+$/).max(128).describe('The package version');
const locale = z.string().regex(/^([a-zA-Z]{2,3}|[iI]-[a-zA-Z]+|[xX]-[a-zA-Z]{1,8})(-[a-zA-Z]{1,8})*$/).max(20);
const manifestType = (type: ManifestType) => z.literal(type).describe('The manifest type');
const manifestVersion = z.string().regex(new RegExp(`^${versionPart}(\\.${versionPart}){2}$`)).describe(
	'The manifest syntax version',
);
const minimumOSVersion = z.string().regex(new RegExp(`^${versionPart}(\\.${versionPart}){0,3}$`));
const url = z.string().regex(/^([Hh][Tt][Tt][Pp][Ss]?):\/\/.+$/).max(2048);
const sha256 = z.string().regex(/^[A-Fa-f0-9]{64}$/);
const installerReturnCode = z.int().min(-2147483648).max(4294967295).refine(code => code !== 0).describe(
	'An exit code that can be returned by the installer after execution',
);

export const versionManifest = z.strictObject({
	PackageIdentifier: packageIdentifier,
	PackageVersion: packageVersion,
	DefaultLocale: locale.describe('The default package meta-data locale'),
	ManifestType: manifestType('version'),
	ManifestVersion: manifestVersion,
});

export const installerManifest = z.strictObject({
	PackageIdentifier: packageIdentifier,
	PackageVersion: packageVersion,
	InstallerLocale: locale.optional().describe('The installer meta-data locale'),
	Platform: z.array(z.enum(['Windows.Desktop', 'Windows.Universal'])).max(2).refine(unique).optional().describe(
		'The installer supported operating system',
	),
	MinimumOSVersion: minimumOSVersion.optional().describe('The installer minimum operating system version'),
	InstallerType: z.enum([
		'msix',
		'msi',
		'appx',
		'exe',
		'zip',
		'inno',
		'nullsoft',
		'wix',
		'burn',
		'pwa',
		'portable',
		'font',
	])
		.optional().describe(
			'Enumeration of supported installer types. InstallerType is required in either root level or individual Installer level',
		),
	NestedInstallerType: z.enum(['msix', 'msi', 'appx', 'exe', 'inno', 'nullsoft', 'wix', 'burn', 'portable', 'font'])
		.optional().describe('Enumeration of supported nested installer types contained inside an archive file'),
	NestedInstallerFiles: z.array(
		z.strictObject({
			RelativeFilePath: z.string().min(1).max(512).describe('The relative path to the nested installer file'),
			PortableCommandAlias: z.string().min(1).max(40).optional().describe(
				'The command alias to be used for calling the package. Only applies to the nested portable package',
			),
		}).describe('A nested installer file contained inside an archive'),
	).max(1024).optional().describe('List of nested installer files contained inside an archive'),
	Scope: z.enum(['user', 'machine']).optional().describe('Scope indicates if the installer is per user or per machine'),
	InstallerSuccessCodes: z.array(installerReturnCode).max(16).refine(unique).optional().describe(
		'List of additional non-zero installer success exit codes other than known default values by winget',
	),
	UpgradeBehavior: z.enum(['install', 'uninstallPrevious', 'deny']).optional().describe('The upgrade method'),
	Commands: z.array(z.string().min(1).max(40)).max(16).refine(unique).optional().describe(
		'List of commands or aliases to run the package',
	),
	ReleaseDate: z.iso.date().optional().describe('The installer release date'),
	InstallationMetadata: z.strictObject({
		DefaultInstallLocation: z.string().min(1).max(2048).optional().describe(
			'Represents the default installed package location. Used for deeper installation detection.',
		),
		Files: z.array(
			z.strictObject({
				RelativeFilePath: z.string().min(1).max(2048).describe('The relative path to the installed file.'),
				FileSha256: sha256.optional().describe('Optional Sha256 of the installed file.'),
				FileType: z.enum(['launch', 'uninstall', 'other']).optional().describe(
					'The optional installed file type. If not specified, the file is treated as other.',
				),
				InvocationParameter: z.string().min(1).max(2048).optional().describe('Optional parameter for invocable files.'),
				DisplayName: z.string().min(1).max(256).optional().describe('Optional display name for invocable files.'),
			}).describe('Represents an installed file.'),
		).max(2048).refine(unique).optional().describe('List of installed files.'),
	}).optional().describe('Details about the installation. Used for deeper installation detection.'),
	Installers: z.array(z.strictObject({
		Architecture: z.enum(['x86', 'x64', 'arm', 'arm64', 'neutral']).describe('The installer target architecture'),
		InstallerUrl: url.describe('The installer Url'),
		InstallerSha256: sha256.describe('Sha256 is required. Sha256 of the installer'),
	})).min(1).max(1024),
	ManifestType: manifestType('installer'),
	ManifestVersion: manifestVersion,
});

export const defaultLocaleManifest = z.strictObject({
	PackageIdentifier: packageIdentifier,
	PackageVersion: packageVersion,
	PackageLocale: locale.describe('The package meta-data locale'),
	Publisher: z.string().min(2).max(256).describe('The publisher name'),
	PublisherUrl: url.optional().describe('The publisher home page'),
	PublisherSupportUrl: url.optional().describe('The publisher support page'),
	Author: z.string().min(2).max(256).optional().describe('The package author'),
	PackageName: z.string().min(2).max(256).describe('The package name'),
	PackageUrl: url.optional().describe('The package home page'),
	License: z.string().min(3).max(512).describe('The package license'),
	LicenseUrl: url.optional().describe('The license page'),
	Copyright: z.string().min(3).max(512).optional().describe('The package copyright'),
	CopyrightUrl: url.optional().describe('The package copyright page'),
	ShortDescription: z.string().min(3).max(256).describe('The short package description'),
	Description: z.string().min(3).max(10000).optional().describe('The full package description'),
	Tags: z.array(z.string().min(1).max(40).describe('Package moniker or tag')).max(16).refine(unique).optional()
		.describe(
			'List of additional package search terms',
		),
	ReleaseNotes: z.string().min(1).max(10000).optional().describe('The package release notes'),
	ReleaseNotesUrl: url.optional().describe('The package release notes url'),
	InstallationNotes: z.string().min(1).max(10000).optional().describe(
		'The notes displayed to the user upon completion of a package installation.',
	),
	Documentations: z.array(z.strictObject({
		DocumentLabel: z.string().min(1).max(100).optional().describe(
			'The label of the documentation for providing software guides such as manuals and troubleshooting URLs.',
		),
		DocumentUrl: url.optional().describe('The documentation URL.'),
	})).max(256).optional(),
	ManifestType: manifestType('defaultLocale'),
	ManifestVersion: manifestVersion,
});

export type VersionManifest = z.infer<typeof versionManifest>;
export type InstallerManifest = z.infer<typeof installerManifest>;
export type DefaultLocaleManifest = z.infer<typeof defaultLocaleManifest>;
