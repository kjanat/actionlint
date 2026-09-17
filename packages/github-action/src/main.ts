import { arch, env, platform as pf } from 'node:process';

import { runnerPlatform } from '#assets';
import { temporary } from '#native';
import { publishTools } from '#path';
import { InputError, runAction } from '#runtime';
import { checkExecutable, executeNative, nativeBinary, pyflakesCommand, shellcheckBinary } from '#tools';
import { commandEscape, writeOutputs } from '#workflow';

declare const __ACTIONLINT_VERSION__: string;

async function main(): Promise<void> {
	const token = env['INPUT_TOKEN']?.trim();
	if (token) console.log(`::add-mask::${commandEscape(token)}`);
	const platform = runnerPlatform(pf, arch);
	const environment = Object.fromEntries(
		Object.entries(env).filter((entry): entry is [string, string] => entry[1] !== undefined),
	);
	process.exitCode = await temporary((directory) =>
		runAction(environment, {
			native: () => nativeBinary(__ACTIONLINT_VERSION__, platform, directory),
			checkExecutable,
			shellcheck: () => shellcheckBinary(platform),
			pyflakes: () => pyflakesCommand(platform),
			publish: (tools) => publishTools(tools, environment),
			execute: executeNative,
		})
	);
}

try {
	await main();
} catch (error) {
	process.exitCode = error instanceof InputError ? 2 : 3;
	try {
		await writeOutputs(env['GITHUB_OUTPUT'], {
			'exit-code': String(process.exitCode),
			'result': error instanceof InputError ? 'invalid-options' : 'failure',
			'problems-found': 'false',
			'problem-count': '',
			'output': '',
			'output-file': '',
		});
	} catch (outputError) {
		console.log(`::error::${commandEscape(outputError instanceof Error ? outputError.message : String(outputError))}`);
	}
	console.log(`::error::${commandEscape(error instanceof Error ? error.message : String(error))}`);
}
