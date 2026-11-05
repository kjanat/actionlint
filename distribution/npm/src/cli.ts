import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

import { cli, command, flag } from '@kjanat/dreamcli';

import { buildPackages } from '#build';

export const build = command('build')
	.description('Build npm packages from checksum-verified release archives')
	.flag('version', flag.string().env('INPUT_VERSION').required().describe('Release version, with or without v'))
	.flag(
		'repository',
		flag.string().env('INPUT_REPOSITORY').describe('GitHub owner/repository for assets missing locally'),
	)
	.flag('token', flag.string().env('INPUT_TOKEN').describe('GitHub token for private release assets'))
	.flag(
		'root',
		flag.string().env('GITHUB_WORKSPACE').default(process.cwd()).describe(
			'Repository containing the licence and schemas',
		),
	)
	.flag('npm-dir', flag.string().env('INPUT_NPM_DIR').describe('Directory containing targets.json and facade sources'))
	.flag('out', flag.string().env('INPUT_OUT_DIR').describe('Directory for generated packages (default: npm-dir/dist)'))
	.flag(
		'downloads',
		flag.string().env('INPUT_DOWNLOADS').describe('Directory of release archives and their checksums file'),
	)
	.flag('only', flag.string().env('INPUT_ONLY').describe('Build one target, its full package name, or facade'))
	.example(
		({ name }) => `${name} build --version 2.0.0 --repository kjanat/actionlint`,
		'Download verified release assets and build every package',
	)
	.example(
		({ name }) => `${name} build --version 2.0.0 --downloads ./assets --only linux-x64`,
		'Build one package from local archives',
	)
	.example(
		({ name }) => `${name} build --version 2.0.0 --downloads ./assets --only facade`,
		'Build the launcher package and its manual',
	)
	.action(async ({ flags, out }) => {
		const root = resolve(flags.root);
		const npmDir = resolve(flags['npm-dir'] || join(root, 'distribution/npm'));
		await buildPackages({
			version: flags.version,
			repository: flags.repository ?? '',
			token: flags.token ?? '',
			repoRoot: root,
			npmDir,
			outDir: resolve(flags.out || join(npmDir, 'dist')),
			downloads: flags.downloads ? resolve(flags.downloads) : '',
			only: flags.only ?? '',
		}, { report: message => out.log(message) });
	});

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	await cli('actionlint-npm').description('Package actionlint release binaries for npm').command(build).run();
}
