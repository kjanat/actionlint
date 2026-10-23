import { isAbsolute } from 'node:path';

import { normalizeEnvironment, subprocessEnvironment } from '#environment';

export type Environment = Record<string, string>;

export class InputError extends Error {}

export type Runtime = {
	native: () => Promise<string>;
	checkExecutable: (path: string) => Promise<void>;
	plugins: (executable: string, environment: Environment, run: () => Promise<number>) => Promise<number>;
	publish: (executable: string) => Promise<void>;
	execute: (executable: string, args: string[], environment: Environment) => Promise<number>;
};

// Parse launcher options; analysis options belong to the native Action parser.
export function toolEnabled(value: string | undefined): boolean | undefined {
	if (value === undefined || value === '' || value === 'true') return true;
	if (value === 'false') return false;
	return undefined;
}

export function installOnly(environment: Environment): boolean {
	const value = toolEnabled(environment['INPUT_INSTALL-ONLY'] || 'false');
	if (value === undefined) throw new InputError("Input 'install-only' must be 'true' or 'false'");
	return value;
}

export async function runAction(environment: Environment, runtime: Runtime): Promise<number> {
	environment = normalizeEnvironment(environment);
	const setupOnly = installOnly(environment);
	const exportInput = (name: string): boolean => {
		const value = toolEnabled(environment[`INPUT_${name.toUpperCase()}`]);
		if (value === undefined) throw new InputError(`Input '${name}' must be 'true' or 'false'`);
		return value;
	};
	const addActionlint = exportInput('add-actionlint-to-path');
	exportInput('plugin-cache');
	if (setupOnly && !addActionlint) {
		throw new InputError("Input 'install-only: true' requires 'add-actionlint-to-path: true'");
	}
	const override = environment.ACTIONLINT_ACTION_BINARY;
	let executable: string;
	if (override) {
		if (!isAbsolute(override)) throw new Error('ACTIONLINT_ACTION_BINARY must be an absolute path');
		await runtime.checkExecutable(override);
		executable = override;
	} else {
		executable = await runtime.native();
	}

	const childEnvironment = subprocessEnvironment(environment);
	// Match action.yml defaults even when the launcher is invoked directly.
	if (!childEnvironment.INPUT_RUFF) childEnvironment.INPUT_RUFF = 'true';
	if (environment.INPUT_PYFLAKES === 'true') {
		console.log('::warning::The deprecated pyflakes input is ignored. Use ruff with a declared Ruff Wasm plugin.');
	}
	childEnvironment.INPUT_PYFLAKES = 'false';
	const run = async (): Promise<number> => {
		if (addActionlint) await runtime.publish(executable);
		if (setupOnly) return 0;
		return runtime.execute(executable, ['-github-action'], childEnvironment);
	};
	return setupOnly ? run() : runtime.plugins(executable, childEnvironment, run);
}
