import assert from 'node:assert/strict';

import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

interface Context {
	directory: string;
	run(args: string[], expected?: number): Promise<string>;
}

const workflow = `on: push
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: echo example${'  '}
permissions: {}
`;

/** Prepare one YAML style violation for the registry's installed-plugin checks. */
export async function prepare({ directory }: Pick<Context, 'directory'>): Promise<void> {
	await mkdir(join(directory, '.github'), { recursive: true });
	await writeFile(
		join(directory, '.github', 'actionlint.yaml'),
		JSON.stringify({
			tools: { ruff: false, shellcheck: false },
			lint: {
				rules: {
					plugins: { yamllint: { level: 'on', options: { config_data: '{rules: {trailing-spaces: enable}}' } } },
				},
			},
		}),
	);
	await writeFile(join(directory, 'workflow.yml'), workflow);
}

function object(value: unknown): Record<string, unknown> {
	assert.ok(value !== null && typeof value === 'object' && !Array.isArray(value));
	return Object.fromEntries(Object.entries(value));
}

/** Run real yamllint through the installed provider and check corrected source. */
export async function verify({ directory, run }: Context): Promise<{ checks: string[]; probe: string[] }> {
	const probe = ['check', '--json', 'workflow.yml'];
	const result = object(JSON.parse(await run(probe, 1)));
	assert.equal(result.completed, true);
	assert.ok(Array.isArray(result.diagnostics));
	assert.equal(result.diagnostics.length, 1);
	const diagnostic = object(result.diagnostics[0]);
	assert.equal(diagnostic.rule, 'yamllint');
	assert.equal(diagnostic.code, 'trailing-spaces');
	assert.deepEqual(diagnostic.start, { line: 6, column: 26 });
	await writeFile(join(directory, 'workflow.yml'), workflow.replace('echo example  ', 'echo example'));
	try {
		const corrected = object(JSON.parse(await run(probe)));
		assert.equal(corrected.completed, true);
		assert.deepEqual(corrected.diagnostics, []);
	} finally {
		await writeFile(join(directory, 'workflow.yml'), workflow);
	}
	return {
		checks: ['installed yamllint and configured rule', 'source range matches YAML', 'correction clears finding'],
		probe,
	};
}
