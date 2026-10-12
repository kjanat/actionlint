import { readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

import { cli, command, flag } from '@kjanat/dreamcli';

import { generateManifests, renderManifests } from '#generate';

export const generate = command('generate')
	.description('Write the version, installer and locale manifests of a release')
	.flag(
		'version',
		flag.string({ pattern: /^\d+\.\d+\.\d+$/ }).env('VERSION').required().describe(
			'Release version, MAJOR.MINOR.PATCH',
		),
	)
	.flag(
		'checksums',
		flag.path({ type: 'file' }).env('WINGET_CHECKSUMS').required().describe('GoReleaser checksums file of the release'),
	)
	.flag(
		'release-notes',
		flag.path({ type: 'file' }).env('WINGET_RELEASE_NOTES').required().describe('Release notes Markdown'),
	)
	.flag(
		'out',
		flag.path({ type: 'directory', create: true }).env('WINGET_OUT').required().describe(
			'Directory the manifests go to',
		),
	)
	.flag(
		'release-date',
		flag.date().env('WINGET_RELEASE_DATE').default(new Date()).describe('ReleaseDate of the installer manifest, UTC'),
	)
	.action(async ({ flags, out }) => {
		const manifests = generateManifests({
			version: flags.version,
			releaseDate: flags['release-date'].toISOString().slice(0, 10),
			checksums: await readFile(flags.checksums, 'utf8'),
			releaseNotes: await readFile(flags['release-notes'], 'utf8'),
		});
		for (const [name, text] of Object.entries(renderManifests(manifests))) {
			const path = join(flags.out, name);
			await writeFile(path, text, { flag: 'wx' });
			out.log(path);
		}
	});

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
	await cli('actionlint-winget').description('WinGet manifests for kjanat.actionlint').command(generate).run();
}
