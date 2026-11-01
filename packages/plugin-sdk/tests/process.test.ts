import { expect, test } from 'bun:test';

import { runCommand } from '#process';

test('subcommands inherit the host-selected environment and accept a complete replacement', async () => {
	const name = 'ACTIONLINT_SDK_PROCESS_CUSTOM';
	const before = process.env[name];
	const command = ['-e', `process.stdout.write(process.env.${name} ?? '<missing>')`];
	try {
		process.env[name] = 'host-selected';
		expect((await runCommand(process.execPath, command)).stdout).toBe('host-selected');
		expect((await runCommand(process.execPath, command, { env: { [name]: 'tool-selected' } })).stdout).toBe(
			'tool-selected',
		);
		expect((await runCommand(process.execPath, command, { env: {} })).stdout).toBe('<missing>');
	} finally {
		if (before === undefined) delete process.env[name];
		else process.env[name] = before;
	}
});
