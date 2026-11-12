import { expect, test } from 'bun:test';

import { runCommand } from '#process';

test('passes shell metacharacters as literal arguments', async () => {
	const literal = 'safe; echo shell-expanded $(echo injected) & %PATH%';
	const result = await runCommand(process.execPath, ['-e', 'process.stdout.write(process.argv[1])', literal], {
		env: {},
	});
	expect(result.stdout).toBe(literal);
});

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
