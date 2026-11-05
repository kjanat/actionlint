import assert from 'node:assert/strict';

import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

interface Context {
	directory: string;
	run(args: string[], expected?: number): Promise<string>;
}

/** Prepare captured PowerShell snippets for the registry's installation checks. */
export async function prepare({ directory }: Pick<Context, 'directory'>): Promise<void> {
	await mkdir(join(directory, '.github'), { recursive: true });
	await writeFile(
		join(directory, '.github', 'actionlint.yaml'),
		'tools:\n  ruff: false\n  shellcheck: false\n  psscriptanalyzer:\n    config:\n      IncludeRules: [PSAvoidUsingCmdletAliases]\n',
	);
	await writeFile(join(directory, 'workflow.yml'), await readFile(new URL('./workflow.yml', import.meta.url)));
}

function object(value: unknown): Record<string, unknown> {
	assert.ok(value !== null && typeof value === 'object' && !Array.isArray(value));
	return Object.fromEntries(Object.entries(value));
}

/** Exercise the installed scanner, UTF-16 source mapping and corrected snippets. */
export async function verify({ directory, run }: Context): Promise<{ checks: string[]; probe: string[] }> {
	const probe = ['check', '--json', 'workflow.yml'];
	const result = object(JSON.parse(await run(probe, 1)));
	assert.equal(result.completed, true);
	assert.ok(Array.isArray(result.diagnostics));
	assert.equal(result.diagnostics.length, 2);
	for (const [index, location] of [{ line: 10, column: 29 }, { line: 14, column: 11 }].entries()) {
		const diagnostic = object(result.diagnostics[index]);
		assert.equal(diagnostic.rule, 'psscriptanalyzer');
		assert.equal(diagnostic.code, 'PSAvoidUsingCmdletAliases');
		assert.equal(diagnostic.severity, 'warning');
		assert.deepEqual(diagnostic.start, location);
		assert.deepEqual(diagnostic.end, { ...location, column: location.column + 3 });
		assert.ok(Array.isArray(diagnostic.fixes));
		assert.equal(diagnostic.fixes.length, 1);
		const fix = object(diagnostic.fixes[0]);
		assert.ok(Array.isArray(fix.edits));
		assert.equal(fix.edits.length, 1);
		const edit = object(fix.edits[0]);
		assert.deepEqual(edit.start, diagnostic.start);
		assert.deepEqual(edit.end, diagnostic.end);
		assert.equal(edit.replacement, 'Get-ChildItem');
	}
	const file = join(directory, 'workflow.yml');
	const source = await readFile(file, 'utf8');
	await writeFile(file, source.replaceAll('gci .', 'Get-ChildItem .'));
	try {
		const corrected = object(JSON.parse(await run(probe)));
		assert.equal(corrected.completed, true);
		assert.deepEqual(corrected.diagnostics, []);
	} finally {
		await writeFile(file, source);
	}
	return {
		checks: [
			'installed PSScriptAnalyzer checks both PowerShell shells',
			'UTF-16 ranges map through YAML',
			'correction clears findings',
		],
		probe,
	};
}
