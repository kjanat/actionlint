import { mkdtemp, readdir, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { runInNewContext } from 'node:vm';

import { expect, test } from 'bun:test';
import { build } from 'tsdown';

test('the core builds for browsers and runs without Node globals', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-sdk-browser-'));
	try {
		await build({
			entry: { sdk: resolve(import.meta.dirname, '../src/index.ts') },
			outDir: directory,
			config: false,
			clean: false,
			dts: false,
			platform: 'browser',
			format: 'iife',
			outputOptions: { name: 'ActionlintSDK' },
			logLevel: 'silent',
		});
		const filename = (await readdir(directory)).find((name) => name.endsWith('.js'));
		if (filename === undefined) throw new Error('browser build emitted no JavaScript');
		const bundle = await readFile(join(directory, filename), 'utf8');
		const output: unknown = await runInNewContext(
			`${bundle}\n(async () => {
 if (typeof process !== 'undefined' || typeof Buffer !== 'undefined') throw new Error('Node globals leaked');
 const result = await ActionlintSDK.executeScriptCheck(({filename}) => [{
  filename, code:'WEB001', message:'Browser callback', location:{row:1,column:1}
 }], JSON.stringify({source:'echo 🙂',filename:'script.sh'}));
 return JSON.stringify(result);
})()`,
			{ TextEncoder },
		);
		expect(typeof output).toBe('string');
		if (typeof output !== 'string') throw new Error('browser test did not return JSON');
		expect(JSON.parse(output)).toEqual({
			ok: true,
			diagnostics: [{
				filename: 'script.sh',
				code: 'WEB001',
				message: 'Browser callback',
				location: { row: 1, column: 1 },
			}],
			errors: [],
		});
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});
