import { strict as assert } from 'node:assert';
import { beforeAll, describe, it } from 'vitest';

import { promises as fs } from 'node:fs';
import { createRequire } from 'node:module';

import { Text } from '@codemirror/state';
import { createChecker } from '@kjanat/rshellcheck-wasm';
import * as pako from 'pako';

import { errorRange } from './range';
import { shellcheckOutput, shellcheckRequest } from './shellcheck-request';
import {
	maxEncodedLength,
	maxSourceBytes,
	maxURLLength,
	querySource,
	readSource,
	sharedSource,
	shareURL,
} from './source';

// The Go wasm runtime installs `globalThis.Go` as a side effect.
import 'wasm_exec.js';

class CheckResults {
	errors: ActionlintError[] | null = null;
	resolve: ((errs: ActionlintError[]) => void) | null = null;

	onCheckCompleted(errs: ActionlintError[]) {
		this.errors = errs;
		if (this.resolve !== null) {
			this.resolve(errs);
			this.resolve = null;
		}
	}

	waitCheckCompleted(): Promise<ActionlintError[]> {
		return new Promise(resolve => {
			if (this.errors !== null) {
				resolve(this.errors);
				return;
			}
			this.resolve = resolve;
		});
	}

	reset(): void {
		this.errors = null;
	}
}

describe('errorRange', () => {
	function error(line: number, column: number, endColumn: number): ActionlintError {
		return { kind: 'k', message: 'm', line, column, endColumn };
	}

	it('maps columns to document offsets', () => {
		const doc = Text.of(['on: push', 'jobs: {}']);
		assert.deepEqual(errorRange(doc, error(2, 1, 4)), { from: 9, to: 13 });
	});

	it('counts an astral character as two code units', () => {
		const doc = Text.of(['# 🚀🚀', 'on: foo']);
		assert.deepEqual(errorRange(doc, error(1, 3, 4)), { from: 2, to: 6 });
	});

	it('clamps an end column past the line to the line end', () => {
		const doc = Text.of(['on: foo', 'jobs: {}']);
		assert.deepEqual(errorRange(doc, error(1, 5, 99)), { from: 4, to: 7 });
	});
});

describe('catalogue startup', () => {
	for (const data of [undefined, '{"version":1}']) {
		it(`refuses ${data === undefined ? 'missing' : 'mismatched'} metadata before exposing linting`, async () => {
			delete window.runActionlint;
			window.actionlintCatalogue = data;
			let message = '';
			let enabled = false;
			window.showError = value => {
				message = value;
			};
			window.dismissLoading = () => {
				enabled = true;
			};
			const go = new Go();
			const bin = await fs.readFile('./public/main.wasm');
			const result = await WebAssembly.instantiate(bin.buffer, go.importObject);
			await go.run(result.instance);
			assert.match(message, /action catalogue/);
			assert.equal(enabled, false, 'failed catalogue must not enable the editor');
			assert.equal(window.runActionlint, undefined, 'failed catalogue must not expose linting');
		});
	}
});

describe('source', () => {
	function encode(bytes: Uint8Array): string {
		return Buffer.from(bytes).toString('base64');
	}

	function shared(text: string): string {
		return encode(pako.deflate(new TextEncoder().encode(text)));
	}

	it('inflates a shared source', () => {
		assert.equal(sharedSource(shared('on: push\n')), 'on: push\n');
	});

	const base = 'https://kjanat.github.io/actionlint/';

	function hashOf(url: string): string {
		assert.ok(url.startsWith(`${base}#`), url.slice(0, 80));
		return url.slice(base.length + 1);
	}

	it('shares a source that the loader accepts', () => {
		assert.equal(sharedSource(hashOf(shareURL(base, 'on: push\n'))), 'on: push\n');
		const limit = 'a'.repeat(maxSourceBytes);
		assert.equal(sharedSource(hashOf(shareURL(base, limit))), limit);
	});

	it('refuses to share a source past the limit', () => {
		assert.throws(() => shareURL(base, 'a'.repeat(maxSourceBytes + 1)), /larger than/);
		assert.throws(() => shareURL(base, 'é'.repeat(maxSourceBytes / 2 + 1)), /larger than/);
	});

	it('refuses to share a URL past the browser limit', () => {
		const letters = 'abcdefghijklmnopqrstuvwxyz';
		const source = Array.from({ length: 2 * maxURLLength }, () => letters[Math.floor(Math.random() * 26)]).join('');
		assert.throws(() => shareURL(base, source), /longer than/);
	});

	it('bounds a query source', () => {
		assert.equal(querySource('on: push\n'), 'on: push\n');
		assert.throws(() => querySource('a'.repeat(maxSourceBytes + 1)), /larger than/);
	});

	it('accepts an incompressible source at the limit', () => {
		const source = new Uint8Array(maxSourceBytes);
		for (let offset = 0; offset < source.length; offset += 65536) {
			crypto.getRandomValues(source.subarray(offset, offset + 65536));
		}
		const encoded = encode(pako.deflate(source));
		assert.ok(encoded.length <= maxEncodedLength, `${encoded.length} > ${maxEncodedLength}`);
		assert.equal(sharedSource(encoded).length > 0, true);
	});

	it('refuses an encoded source past the limit before decoding it', () => {
		assert.throws(() => sharedSource('A'.repeat(maxEncodedLength + 1)), /longer than/);
	});

	it('stops inflating past the source limit', () => {
		assert.throws(() => sharedSource(encode(pako.deflate(new Uint8Array(maxSourceBytes + 1)))), /inflates past/);
	});

	it('rejects a truncated stream', () => {
		const compressed = pako.deflate(new TextEncoder().encode('on: push\n'));
		assert.throws(
			() => sharedSource(encode(compressed.subarray(0, compressed.length - 4))),
			/not a complete zlib stream/,
		);
	});

	it('rejects data after the stream', () => {
		const compressed = pako.deflate(new TextEncoder().encode('on: push\n'));
		assert.throws(() => sharedSource(encode(new Uint8Array([...compressed, 0, 1, 2]))), /3 bytes after/);
	});

	it('reads a remote source', async () => {
		assert.equal(await readSource(new Response('on: push\n')), 'on: push\n');
		assert.equal(await readSource(new Response(null)), '');
	});

	it('stops reading past the source limit', async () => {
		await assert.rejects(readSource(new Response(new Uint8Array(maxSourceBytes + 1))), /larger than/);
	});
});

describe('main.wasm', () => {
	const results = new CheckResults();

	beforeAll(async () => {
		const wasm = await fs.readFile(createRequire(import.meta.url).resolve('@kjanat/rshellcheck-wasm/wasm'));
		const checker = await createChecker(wasm);
		window.runShellcheck = (args, script) =>
			Promise.resolve(shellcheckOutput(checker.check(shellcheckRequest(args, script))));
		window.dismissLoading = () => {
			/*do nothing*/
		};
		window.getYamlSource = () => `
on: push

jobs:
  test:
    steps:
      - run: echo 'hi'`;
		window.onCheckCompleted = results.onCheckCompleted.bind(results);

		const go = new Go();
		window.actionlintCatalogue = await fs.readFile('../internal/actioncatalog/popular_actions.json', 'utf8');
		const bin = await fs.readFile('./public/main.wasm');
		const catalogue = await fs.readFile('../internal/actioncatalog/popular_actions.json');
		assert.equal(bin.includes(catalogue), false, 'site WASM must not embed the JSON catalogue');
		const result = await WebAssembly.instantiate(bin.buffer, go.importObject);

		// This promise is never settled, so it must not be awaited.
		void go.run(result.instance);
	});

	it('shows first result on loading', async () => {
		const errors = await results.waitCheckCompleted();

		const json = JSON.stringify(errors);
		assert.equal(errors.length, 1, json);

		const [err] = errors;
		assert.ok(err, json);
		assert.equal(err.message, '"runs-on" section is missing in job "test"', `message is unexpected: ${json}`);
		assert.equal(err.line, 5, `line is unexpected: ${json}`);
		assert.equal(err.column, 3, `column is unexpected: ${json}`);
		assert.equal(err.kind, 'syntax-check', `kind is unexpected: ${json}`);
	});

	it('reports some errors by running actionlint with runActionlint', async () => {
		assert.ok(window.runActionlint);
		results.reset();

		const source = `
on: foo

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hi'`;

		window.runActionlint(source);
		const errors = await results.waitCheckCompleted();
		const json = JSON.stringify(errors);
		assert.equal(errors.length, 1, json);

		const [err] = errors;
		assert.ok(err, json);
		assert.ok(err.message.includes('unknown Webhook event "foo"'), `message is unexpected: ${json}`);
		assert.equal(err.line, 2, `line is unexpected: ${json}`);
		assert.equal(err.column, 5, `column is unexpected: ${json}`);
		// Columns 5 to 7 are "foo", the range the editor underlines.
		assert.equal(err.endColumn, 7, `end column is unexpected: ${json}`);
		assert.equal(err.kind, 'events', `kind is unexpected: ${json}`);
	});

	it('validates action inputs using the separately loaded catalogue', async () => {
		assert.ok(window.runActionlint);
		results.reset();
		window.runActionlint(
			'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v7\n        with:\n          definitely-not-an-input: true\n',
		);
		const errors = await results.waitCheckCompleted();
		const metadataErrors = errors.filter(error =>
			error.kind === 'action' && error.message.includes('definitely-not-an-input')
		);
		assert.equal(metadataErrors.length, 1, JSON.stringify(errors));
	});

	it('reports no error by running actionlint with runActionlint', async () => {
		assert.ok(window.runActionlint);
		results.reset();

		const source = `
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hi'`;

		window.runActionlint(source);
		const errors = await results.waitCheckCompleted();
		const json = JSON.stringify(errors);
		assert.equal(errors.length, 0, json);
	});

	async function checkRun(run: string): Promise<ActionlintError[]> {
		assert.ok(window.runActionlint);
		results.reset();
		window.runActionlint(`on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n${run}\n`);
		return results.waitCheckCompleted();
	}

	it('maps real ShellCheck findings through literal and quoted YAML scalars', async () => {
		for (const run of ['      - run: |\n          echo $value', '      - run: "echo $value"']) {
			const errors = await checkRun(run);
			const findings = errors.filter(error => error.kind === 'shellcheck');
			assert.equal(findings.length, 1, JSON.stringify(errors));
			const finding = findings[0];
			assert.ok(finding);
			assert.match(finding.message, /SC2086/);
			const line = run.split('\n').at(-1);
			assert.ok(line);
			assert.equal(finding.line, run.includes('|') ? 7 : 6);
			assert.equal(finding.column, line.indexOf('$value') + 1);
			assert.equal(finding.endColumn, finding.column + '$value'.length - 1);
		}
	});

	it('keeps native and actionlint suppressions, expression sanitization, and non-shell scripts', async () => {
		for (
			const run of [
				'      - run: |\n          # shellcheck disable=SC2086\n          echo $value',
				'      # actionlint:ignore-next-line shellcheck\n      - run: echo $value',
				'      - run: echo "${{ github.ref }}"',
				'      - shell: python\n        run: print("hello")',
				'      - shell: pwsh\n        run: Write-Output $value',
			]
		) {
			const errors = await checkRun(run);
			assert.deepEqual(errors, [], JSON.stringify({ run, errors }));
		}
	});

	it('uses workflow shell defaults for portability diagnostics', async () => {
		const errors = await checkRun('      - shell: sh\n        run: "[[ -n x ]]"');
		assert.ok(
			errors.some(error => error.kind === 'shellcheck' && /SC3010/.test(error.message)),
			JSON.stringify(errors),
		);
	});

	it('does not publish stale diagnostics when a newer edit finishes first', async () => {
		assert.ok(window.runActionlint);
		const real = window.runShellcheck;
		let release: ((output: string) => void) | undefined;
		try {
			window.runShellcheck = () =>
				new Promise(resolve => {
					release = resolve;
				});
			results.reset();
			window.runActionlint(
				'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $value\n',
			);
			await new Promise(resolve => setTimeout(resolve, 0));
			assert.ok(release, 'first analysis must reach the asynchronous checker');
			window.runShellcheck = real;
			const current = await checkRun('      - run: echo "ok"');
			release('{"comments":[]}');
			await new Promise(resolve => setTimeout(resolve, 0));
			assert.equal(results.errors, current, 'superseded analysis must not replace newer results');
		} finally {
			window.runShellcheck = real;
		}
	});

	it('discards both stale successes and failures immediately on edit, before another lint starts', async () => {
		assert.ok(window.runActionlint);
		assert.ok(window.invalidateActionlint);
		const real = window.runShellcheck;
		const showError = window.showError;
		const failures: string[] = [];
		try {
			window.showError = message => failures.push(message);
			for (const reject of [false, true]) {
				let finish: (() => void) | undefined;
				window.runShellcheck = () =>
					new Promise((resolve, fail) => {
						finish = () => {
							if (reject) fail(new Error('obsolete timeout'));
							else resolve('{"comments":[]}');
						};
					});
				results.reset();
				window.runActionlint(
					'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n',
				);
				await new Promise(resolve => setTimeout(resolve, 0));
				assert.ok(finish, 'analysis must reach the asynchronous checker');
				window.invalidateActionlint();
				finish();
				await new Promise(resolve => setTimeout(resolve, 0));
				assert.deepEqual(failures, [], 'an edit must invalidate errors during debounce');
				assert.equal(results.errors, null, 'an edit must invalidate results during debounce');
			}
		} finally {
			window.runShellcheck = real;
			window.showError = showError;
		}
	});

	it('reports a rejected worker without publishing a clean result', async () => {
		assert.ok(window.runActionlint);
		const real = window.runShellcheck;
		const showError = window.showError;
		try {
			window.runShellcheck = () => Promise.reject(new Error('test worker timeout'));
			const failure = new Promise<string>(resolve => {
				window.showError = resolve;
			});
			results.reset();
			window.runActionlint('on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n');
			assert.match(await failure, /test worker timeout/);
			assert.equal(results.errors, null, 'failed analysis must not publish a clean result');
		} finally {
			window.runShellcheck = real;
			window.showError = showError;
		}
	});
});
