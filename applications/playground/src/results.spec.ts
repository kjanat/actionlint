import { strict as assert } from 'node:assert';
import { afterAll, beforeAll, it, type Mock, vi } from 'vitest';

import { diagnosticCount } from '@codemirror/lint';
import { EditorView } from '@codemirror/view';
import type { BrowserClientOptions, PublishDiagnosticsParams } from '@kjanat-actionlint/language-server';

import type { PlaygroundClient } from '#client';

const callbacks = vi.hoisted((): {
	options: Pick<BrowserClientOptions, 'onDiagnostics' | 'onError'> | undefined;
	version: number;
	change: Mock<PlaygroundClient['change']>;
	open: Mock<PlaygroundClient['open']>;
	actions: Mock<PlaygroundClient['codeActions']>;
} => {
	const options: Pick<BrowserClientOptions, 'onDiagnostics' | 'onError'> | undefined = undefined;
	return {
		options,
		version: 1,
		change: vi.fn<PlaygroundClient['change']>().mockResolvedValue(),
		open: vi.fn<PlaygroundClient['open']>().mockResolvedValue(),
		actions: vi.fn<PlaygroundClient['codeActions']>().mockResolvedValue([]),
	};
});
vi.mock('#client', () => ({
	documentURI: 'file:///workspace/.github/workflows/playground.yml',
	startPlaygroundClient: (options: Pick<BrowserClientOptions, 'onDiagnostics' | 'onError'>) => {
		callbacks.options = options;
		callbacks.change.mockImplementation((_uri, _text, version) => {
			callbacks.version = version;
			return Promise.resolve();
		});
		return Promise.resolve({
			open: callbacks.open,
			change: callbacks.change,
			codeActions: callbacks.actions,
			complete: vi.fn().mockResolvedValue(null),
			stop: vi.fn().mockResolvedValue(undefined),
		});
	},
}));
vi.mock('#shellcheck', () => ({
	shellcheckAssets: { license: 'https://example.test/LICENSE' },
}));

let editor: EditorView;
let body: HTMLElement;
let error: HTMLElement;
let cleanup = () => {};

beforeAll(async () => {
	document.body.replaceChildren();
	const elements: [keyof HTMLElementTagNameMap, string][] = [
		['div', 'editor'],
		['div', 'loading'],
		['div', 'error-msg'],
		['div', 'success-msg'],
		['button', 'check-url-btn'],
		['button', 'permalink-btn'],
		['input', 'check-url-input'],
		['p', 'invalid-input'],
		['a', 'shellcheck-license'],
	];
	for (const [tag, id] of elements) {
		const element = document.createElement(tag);
		element.id = id;
		if (element instanceof HTMLButtonElement) element.type = 'button';
		if (element instanceof HTMLInputElement) element.type = 'text';
		document.body.append(element);
	}
	const table = document.createElement('table');
	const tbody = document.createElement('tbody');
	tbody.id = 'lint-result-body';
	table.append(tbody);
	document.body.append(table);
	vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, addListener() {}, removeListener() {} }));
	vi.stubGlobal('fetch', () => Promise.resolve(new Response('')));
	Object.defineProperty(Range.prototype, 'getClientRects', {
		configurable: true,
		value: () => [new DOMRect(0, 0, 10, 20)],
	});
	Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
		configurable: true,
		value: () => new DOMRect(0, 0, 10, 20),
	});
	await import('#index');
	await vi.waitFor(() => {
		assert.equal(callbacks.open.mock.calls.length, 1);
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

function currentVersion(): number {
	return callbacks.version;
}

function publication(): PublishDiagnosticsParams {
	return {
		uri: 'file:///workspace/.github/workflows/playground.yml',
		version: currentVersion(),
		diagnostics: [
			{
				range: { start: { line: 0, character: 0 }, end: { line: 0, character: 1 } },
				code: 'shellcheck/SC2086',
				severity: 2,
				message: 'previous finding',
			},
		],
	};
}

async function populate(): Promise<void> {
	assert.ok(callbacks.options);
	callbacks.options.onDiagnostics(publication());
	await vi.waitFor(() => {
		assert.equal(body.children.length, 1, error.textContent);
		assert.equal(diagnosticCount(editor.state), 1);
	});
}

it('displays the severity published by the LSP', async () => {
	await populate();
	assert.equal(body.firstElementChild?.getAttribute('data-severity'), 'warning');
});

it('clears stale results on failure and does not turn a failed analysis into all clear', async () => {
	await populate();
	const tags = body.querySelectorAll('.tag');
	assert.equal(tags.item(tags.length - 1).textContent, 'shellcheck/SC2086');
	assert.ok(callbacks.options);
	callbacks.options.onError('worker failed');
	assert.equal(body.children.length, 0, 'failed analysis must clear clickable stale locations');
	assert.equal(diagnosticCount(editor.state), 0);
	assert.equal(error.textContent, 'worker failed');
	assert.equal(error.style.display, 'block');
	callbacks.options.onDiagnostics({ ...publication(), diagnostics: [] });
	assert.equal(document.getElementById('success-msg')?.style.display, 'none');
	assert.equal(error.style.display, 'block');
});

it('sends a versioned didChange and clears old locations immediately', async () => {
	editor.dispatch({ changes: { from: 0, insert: '# edit\n' } });
	await populate();
	const old = publication();
	const previousCalls = callbacks.change.mock.calls.length;
	editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: 'on: push\njobs: {}' } });
	assert.equal(callbacks.change.mock.calls.length, previousCalls + 1);
	assert.equal(body.children.length, 0);
	assert.equal(diagnosticCount(editor.state), 0);
	assert.equal(error.style.display, 'none');
	assert.ok(callbacks.options);
	callbacks.options.onDiagnostics(old);
	await Promise.resolve();
	assert.equal(body.children.length, 0, 'old document versions must not be displayed');
});

it('applies source-backed versioned LSP quick fixes through the editor', async () => {
	const input = publication();
	callbacks.actions.mockResolvedValueOnce([{
		title: 'Replace first character',
		kind: 'quickfix',
		diagnostics: input.diagnostics,
		edit: {
			documentChanges: [{
				textDocument: { uri: input.uri, version: currentVersion() },
				edits: [{
					range: input.diagnostics[0]?.range ?? { start: { line: 0, character: 0 }, end: { line: 0, character: 1 } },
					newText: 'X',
				}],
			}],
		},
	}]);
	await populate();
	const button = body.querySelector('button');
	assert.ok(button);
	button.click();
	assert.equal(editor.state.doc.sliceString(0, 1), 'X');
	assert.equal(body.children.length, 0, 'applying the edit invalidates old findings');
});
