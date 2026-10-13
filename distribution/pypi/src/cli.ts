import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

import { cli, command, flag } from '@kjanat/dreamcli';

import { buildWheels } from '#wheel';

export const build = command('build')
	.description('Write one wheel per platform tag from the release archives')
	.flag(
		'version',
		flag.string({ pattern: /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/ }).required().describe(
			'Release version, MAJOR.MINOR.PATCH',
		),
	)
	.flag(
		'assets',
		flag.path({ type: 'directory' }).required().describe('Directory with the checksums file and release archives'),
	)
	.flag(
		'out',
		flag.path({ type: 'directory', create: true }).required().describe('Directory the wheels go to'),
	)
	.action(async ({ flags, out }) => {
		for (const wheel of await buildWheels(flags.version, flags.assets)) {
			const path = join(flags.out, wheel.name);
			await writeFile(path, wheel.data, { flag: 'wx' });
			out.log(path);
		}
	});

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
	await cli('actionlint-pypi').description('PyPI wheels for actionlint-kjanat').command(build).run();
}
