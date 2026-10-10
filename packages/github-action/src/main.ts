import { arch, env, platform as pf } from 'node:process';

import { runnerPlatform } from '#assets';
import { normalizeEnvironment } from '#environment';
import { temporary } from '#native';
import { publishTools } from '#path';
import { withReporting } from '#reporters';
import type { Environment } from '#runtime';
import { InputError, runAction } from '#runtime';
import { checkExecutable, executeNative, inspectTools, nativeBinary, ruffBinary, shellcheckBinary } from '#tools';
import { selectedVersion } from '#version';
import { commandEscape, writeOutputs } from '#workflow';

declare const __ACTIONLINT_VERSION__: string;

const environment = normalizeEnvironment(env);

async function main(): Promise<void> {
	const token = environment['INPUT_TOKEN']?.trim();
	if (token) console.log(`::add-mask::${commandEscape(token)}`);
	const execute = async (childEnvironment: Environment): Promise<number> => {
		const platform = runnerPlatform(pf, arch);
		return temporary((directory) =>
			runAction(childEnvironment, {
				native: async () => {
					const version = await selectedVersion(environment, __ACTIONLINT_VERSION__);
					const executable = await nativeBinary(version, platform, directory);
					await writeOutputs(environment.GITHUB_OUTPUT, { version });
					return executable;
				},
				checkExecutable,
				inspect: inspectTools,
				shellcheck: () => shellcheckBinary(platform),
				ruff: () => ruffBinary(platform),
				publish: (tools) => publishTools(tools, environment),
				execute: executeNative,
			})
		);
	};
	if (environment['INPUT_INSTALL-ONLY'] === 'true') {
		process.exitCode = await execute(environment);
		await writeOutputs(environment.GITHUB_OUTPUT, {
			'exit-code': '0',
			'result': 'success',
			'problems-found': 'false',
			'problem-count': '',
			'output': '',
			'output-file': '',
			'result-file': '',
			'report-sarif': '',
		});
	} else {
		process.exitCode = await withReporting(environment, execute);
	}
}

try {
	await main();
} catch (error) {
	// Emergency fallback if result initialization or finalization itself failed.
	process.exitCode = error instanceof InputError ? 2 : 3;
	try {
		await writeOutputs(environment['GITHUB_OUTPUT'], {
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
