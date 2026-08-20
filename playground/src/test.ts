import { Text } from '@codemirror/state';
import { strict as assert } from 'node:assert';
import { promises as fs } from 'node:fs';
import { beforeAll, describe, it } from 'vitest';

import { errorRange } from './range';

// The Go wasm runtime installs `globalThis.Go` as a side effect.
import '../public/wasm_exec.js';

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

describe('errorRange', function() {
	function error(line: number, column: number, endColumn: number): ActionlintError {
		return { kind: 'k', message: 'm', line, column, endColumn };
	}

	it('maps columns to document offsets', function() {
		const doc = Text.of(['on: push', 'jobs: {}']);
		assert.deepEqual(errorRange(doc, error(2, 1, 4)), { from: 9, to: 13 });
	});

	it('counts an astral character as two code units', function() {
		const doc = Text.of(['# 🚀🚀', 'on: foo']);
		assert.deepEqual(errorRange(doc, error(1, 3, 4)), { from: 2, to: 6 });
	});

	it('clamps an end column past the line to the line end', function() {
		const doc = Text.of(['on: foo', 'jobs: {}']);
		assert.deepEqual(errorRange(doc, error(1, 5, 99)), { from: 4, to: 7 });
	});
});

describe('main.wasm', function() {
	const results = new CheckResults();

	beforeAll(async function() {
		window.dismissLoading = function() {
			/*do nothing*/
		};
		window.getYamlSource = function() {
			return `
on: push

jobs:
  test:
    steps:
      - run: echo 'hi'`;
		};
		window.onCheckCompleted = results.onCheckCompleted.bind(results);

		const go = new Go();
		const bin = await fs.readFile('./public/main.wasm');
		const result = await WebAssembly.instantiate(bin.buffer, go.importObject);

		// This promise is never settled, so it must not be awaited.
		void go.run(result.instance);
	});

	it('shows first result on loading', async function() {
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

	it('reports some errors by running actionlint with runActionlint', async function() {
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

	it('reports no error by running actionlint with runActionlint', async function() {
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
});
