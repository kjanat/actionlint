import assert from 'node:assert/strict';

import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

interface Context {
	directory: string;
	run(args: string[], expected?: number): Promise<string>;
}

const workflow = `name: lint
on: push
concurrency:
  group: lint
  cancel-in-progress: true
permissions:
  contents: read
jobs:
  lint:
    name: lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          persist-credentials: false
`;

/** Prepare an unpinned action reference for the registry's installed-plugin checks. */
export async function prepare({ directory }: Pick<Context, 'directory'>): Promise<void> {
	await mkdir(join(directory, '.github'), { recursive: true });
	await writeFile(
		join(directory, '.github', 'actionlint.yaml'),
		JSON.stringify({
			tools: { ruff: false, shellcheck: false },
			lint: { rules: { plugins: { zizmor: { level: 'on', options: { persona: 'auditor' } } } } },
		}),
	);
	await writeFile(join(directory, 'workflow.yml'), workflow);
}

function object(value: unknown): Record<string, unknown> {
	assert.ok(value !== null && typeof value === 'object' && !Array.isArray(value));
	return Object.fromEntries(Object.entries(value));
}

/** Run offline zizmor through the installed provider, then verify a pinned reference. */
export async function verify({ directory, run }: Context): Promise<{ checks: string[]; probe: string[] }> {
	const probe = ['check', '--json', 'workflow.yml'];
	const result = object(JSON.parse(await run(probe, 1)));
	assert.equal(result.completed, true);
	assert.ok(Array.isArray(result.diagnostics));
	assert.equal(result.diagnostics.length, 1);
	const diagnostic = object(result.diagnostics[0]);
	assert.equal(diagnostic.rule, 'zizmor');
	assert.equal(diagnostic.code, 'zizmor/unpinned-uses');
	assert.deepEqual(diagnostic.start, { line: 13, column: 15 });
	assert.deepEqual(diagnostic.end, { line: 13, column: 34 });
	await writeFile(
		join(directory, 'workflow.yml'),
		workflow.replace('actions/checkout@v4', 'actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1'),
	);
	try {
		const corrected = object(JSON.parse(await run(probe)));
		assert.equal(corrected.completed, true);
		assert.deepEqual(corrected.diagnostics, []);
	} finally {
		await writeFile(join(directory, 'workflow.yml'), workflow);
	}
	return {
		checks: [
			'installed offline zizmor and SARIF mapping',
			'action reference range matches YAML',
			'pinning clears finding',
		],
		probe,
	};
}
