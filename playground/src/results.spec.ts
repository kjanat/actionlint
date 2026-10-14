import { strict as assert } from 'node:assert';
import { afterAll, beforeAll, it, vi } from 'vitest';

import { readFileSync } from 'node:fs';

import { diagnosticCount } from '@codemirror/lint';
import { EditorView } from '@codemirror/view';

const callbacks = vi.hoisted(() => ({ cancel: vi.fn(), invalidate: vi.fn(), lint: vi.fn() }));
vi.mock('wasm_exec.js', () => ({}));
vi.mock('./catalogue.ts', () => ({ fetchActionCatalogue: () => Promise.resolve('test catalogue') }));
vi.mock('./shellcheck', () => ({
	loadShellcheck: () => Promise.resolve({ check: vi.fn(), cancel: callbacks.cancel }),
	shellcheckAssets: { license: 'https://example.test/LICENSE' },
}));

let editor: EditorView;
let body: HTMLElement;
let error: HTMLElement;
let cleanup = () => {};

beforeAll(async () => {
	document.body.innerHTML = readFileSync('index.html', 'utf8');
	vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, addListener() {}, removeListener() {} }));
	vi.stubGlobal('fetch', () => Promise.resolve(new Response('')));
	vi.stubGlobal(
		'Go',
		class {
			importObject = {};
			run() {
				window.runActionlint = callbacks.lint;
				window.invalidateActionlint = callbacks.invalidate;
				window.dismissLoading();
				return Promise.resolve();
			}
		},
	);
	const module = new WebAssembly.Module(new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0]));
	vi.spyOn(WebAssembly, 'instantiateStreaming').mockResolvedValue({
		module,
		instance: new WebAssembly.Instance(module),
	});
	await import('./index');
	await vi.waitFor(() => {
		assert.equal(typeof window.runActionlint, 'function');
	});
	const element = document.querySelector('.cm-editor');
	assert.ok(element instanceof HTMLElement);
	const view = EditorView.findFromDOM(element);
	assert.ok(view);
	editor = view;
	cleanup = () => {
		view.destroy();
	};
	const rows = document.getElementById('lint-result-body');
	const message = document.getElementById('error-msg');
	assert.ok(rows);
	assert.ok(message);
	body = rows;
	error = message;
});

afterAll(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
});

function populate(): void {
	window.onCheckCompleted([
		{ line: 1, column: 1, endColumn: 2, kind: 'shellcheck', message: 'previous finding' },
	], window.getYamlSource());
	assert.equal(body.children.length, 1, 'fixture must populate a clickable result');
	assert.equal(diagnosticCount(editor.state), 1, 'fixture must populate editor diagnostics');
}

it('clears old clickable rows and editor diagnostics when analysis fails', () => {
	populate();
	window.showError('worker failed');
	assert.equal(body.children.length, 0, 'failed analysis must not retain clickable stale locations');
	assert.equal(diagnosticCount(editor.state), 0);
	assert.equal(error.textContent, 'worker failed');
	assert.equal(error.style.display, 'block');
});

it('invalidates, cancels and clears immediately on edit before the debounced lint', async () => {
	populate();
	editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: 'on: push\njobs: {}' } });
	assert.equal(callbacks.invalidate.mock.calls.length, 1);
	assert.equal(callbacks.cancel.mock.calls.length, 1);
	const invalidatedAt = callbacks.invalidate.mock.invocationCallOrder[0];
	const cancelledAt = callbacks.cancel.mock.invocationCallOrder[0];
	assert.ok(invalidatedAt && cancelledAt && invalidatedAt < cancelledAt, 'invalidate before rejecting worker requests');
	assert.equal(callbacks.lint.mock.calls.length, 0, 'lint should still be debounced');
	assert.equal(body.children.length, 0);
	assert.equal(diagnosticCount(editor.state), 0);
	assert.equal(error.style.display, 'none');
	await vi.waitFor(() => {
		assert.equal(callbacks.lint.mock.calls.length, 1);
	});
});
