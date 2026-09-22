import { isAbsolute } from 'node:path';

import { normalizeEnvironment, subprocessEnvironment } from '#environment';

export type Environment = Record<string, string>;

export class InputError extends Error {}

export type PyflakesCommand =
	| { kind: 'command'; executable: string }
	| { kind: 'python'; executable: string; script: string };

export type InstalledTools = {
	actionlint?: string;
	shellcheck?: string;
	pyflakes?: PyflakesCommand;
};

export type ToolRequirements = { shellcheck: boolean; pyflakes: boolean };

export type Runtime = {
	native: () => Promise<string>;
	checkExecutable: (path: string) => Promise<void>;
	inspect: (executable: string, environment: Environment) => Promise<ToolRequirements>;
	shellcheck: () => Promise<string>;
	pyflakes: () => Promise<PyflakesCommand>;
	publish: (tools: InstalledTools) => Promise<void>;
	execute: (executable: string, args: string[], environment: Environment) => Promise<number>;
};

// Leave invalid inputs to the native action parser, which writes the established
// invalid-options outputs. Never install optional tools for an invalid boolean.
export function toolEnabled(value: string | undefined): boolean | undefined {
	if (value === undefined || value === '' || value === 'true') return true;
	if (value === 'false') return false;
	return undefined;
}

export async function runAction(environment: Environment, runtime: Runtime): Promise<number> {
	environment = normalizeEnvironment(environment);
	const exportInput = (name: string): boolean => {
		const value = toolEnabled(environment[`INPUT_${name.toUpperCase()}`]);
		if (value === undefined) throw new InputError(`Input '${name}' must be 'true' or 'false'`);
		return value;
	};
	const addActionlint = exportInput('add-actionlint-to-path');
	const addShellcheck = exportInput('add-shellcheck-to-path');
	const addPyflakes = exportInput('add-pyflakes-to-path');
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
	const tools: InstalledTools = {};
	if (addActionlint) tools.actionlint = executable;
	delete childEnvironment.ACTIONLINT_SHELLCHECK_COMMAND;
	delete childEnvironment.ACTIONLINT_PYFLAKES_COMMAND;
	delete childEnvironment.ACTIONLINT_PYTHON;
	delete childEnvironment.ACTIONLINT_PYFLAKES_SCRIPT;
	const shellcheck = toolEnabled(environment.INPUT_SHELLCHECK);
	const pyflakes = toolEnabled(environment.INPUT_PYFLAKES);
	if (shellcheck !== undefined && pyflakes !== undefined) {
		const needed = await runtime.inspect(executable, childEnvironment);
		if (shellcheck && needed.shellcheck) {
			const command = await runtime.shellcheck();
			if (addShellcheck) tools.shellcheck = command;
			childEnvironment.ACTIONLINT_SHELLCHECK_COMMAND = command;
		}
		if (pyflakes && needed.pyflakes) {
			const command = await runtime.pyflakes();
			if (addPyflakes) tools.pyflakes = command;
			if (command.kind === 'command') {
				childEnvironment.ACTIONLINT_PYFLAKES_COMMAND = command.executable;
			} else {
				childEnvironment.ACTIONLINT_PYTHON = command.executable;
				childEnvironment.ACTIONLINT_PYFLAKES_SCRIPT = command.script;
			}
		}
		if (Object.keys(tools).length > 0) await runtime.publish(tools);
	}
	return runtime.execute(executable, ['-github-action'], childEnvironment);
}
