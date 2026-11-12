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
      - shell: bash
        run: |
          echo example${'  '}
`;

/** Prepare an isolated project for the registry's candidate and delivery checks. */
export async function prepare({ directory }: Pick<Context, 'directory'>): Promise<void> {
	await mkdir(join(directory, '.github'), { recursive: true });
	await writeFile(join(directory, '.github', 'actionlint.yaml'), 'tools:\n  ruff: false\n  shellcheck: false\n');
	await writeFile(join(directory, 'workflow.yml'), workflow);
}

function object(value: unknown): Record<string, unknown> {
	assert.ok(value !== null && typeof value === 'object' && !Array.isArray(value));
	return Object.fromEntries(Object.entries(value));
}

/** Verify execution, source mapping and a correction through the installed plugin. */
export async function verify({ directory, run }: Context): Promise<{ checks: string[]; probe: string[] }> {
	const probe = ['check', '--json', 'workflow.yml'];
	const result = object(JSON.parse(await run(probe, 1)));
	assert.equal(result.completed, true);
	assert.ok(Array.isArray(result.diagnostics));
	assert.equal(result.diagnostics.length, 1);
	const diagnostic = object(result.diagnostics[0]);
	assert.equal(diagnostic.rule, 'trailing-space');
	assert.equal(object(diagnostic.start).line, 8);
	await writeFile(join(directory, 'workflow.yml'), workflow.replace('echo example  ', 'echo example'));
	try {
		const corrected = object(JSON.parse(await run(probe)));
		assert.equal(corrected.completed, true);
		assert.deepEqual(corrected.diagnostics, []);
	} finally {
		await writeFile(join(directory, 'workflow.yml'), workflow);
	}
	return { checks: ['installed callback and mapped diagnostic', 'source correction clears diagnostic'], probe };
}
