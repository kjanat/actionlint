import 'bulma/css/bulma.min.css';
import 'devicon/devicon.min.css';
import './style.css';

import { indentWithTab } from '@codemirror/commands';
import { yaml } from '@codemirror/lang-yaml';
import type { Diagnostic } from '@codemirror/lint';
import { lintGutter, setDiagnostics } from '@codemirror/lint';
import type { Extension } from '@codemirror/state';
import { Compartment } from '@codemirror/state';
import { oneDark } from '@codemirror/theme-one-dark';
import { EditorView, keymap } from '@codemirror/view';
import { basicSetup } from 'codemirror';
import isMobile from 'ismobilejs';
import * as pako from 'pako';

import { errorRange } from './range';

const editorTheme = new Compartment();

function themeFor(isDark: boolean): Extension {
	return isDark ? oneDark : [];
}

// Spreading the whole array into String.fromCharCode throws RangeError past about 128 KiB.
function toBase64(bytes: Uint8Array): string {
	let s = '';
	for (const b of bytes) {
		s += String.fromCharCode(b);
	}
	return btoa(s);
}

(async function() {
	function getElementById(id: string): HTMLElement {
		const e = document.getElementById(id);
		if (e === null) {
			throw new Error(`#${id} element does not exist`);
		}
		return e;
	}

	const body = getElementById('lint-result-body');
	const errorMessage = getElementById('error-msg');
	const successMessage = getElementById('success-msg');
	const nowLoading = getElementById('loading');
	const checkUrlButton = getElementById('check-url-btn');
	const checkUrlInput = getElementById('check-url-input') as HTMLInputElement;
	const permalinkButton = getElementById('permalink-btn');
	const invalidInputMessage = getElementById('invalid-input');
	const preferDark = window.matchMedia('(prefers-color-scheme: dark)');

	async function getRemoteSource(url: string): Promise<string> {
		function getUrlToFetch(u: string): string {
			const url = new URL(u);

			// Convert repository URL to raw source URL
			if (url.host === 'github.com') {
				// Convert /owner/repo/blob/branch/path/to to /owner/repo/branch/path/to
				const s = url.pathname.split('/blob/');
				if (s.length === 2) {
					url.pathname = s.join('/');
					url.host = 'raw.githubusercontent.com';
					return url.toString();
				}
			}

			// Convert Gist URL to raw source URL
			if (url.host === 'gist.github.com' && /\/[0-9a-f]+$/.test(url.pathname)) {
				url.host = 'gist.githubusercontent.com';
				url.pathname += '/raw';
				return url.toString();
			}

			return u;
		}

		const res = await fetch(getUrlToFetch(url));
		if (!res.ok) {
			throw new Error(`Fetching ${url} failed with status ${res.status}: ${res.statusText}`);
		}
		const src = await res.text();
		return src.trim();
	}

	async function getDefaultSource(): Promise<string> {
		const params = new URLSearchParams(window.location.search);

		const s = params.get('s');
		if (s !== null) {
			return s;
		}

		const u = params.get('u');
		if (u !== null) {
			return getRemoteSource(u);
		}

		if (window.location.hash !== '') {
			const b64 = window.location.hash.slice(1); // Omit first '#'
			const compressed = Uint8Array.from(atob(b64), c => c.charCodeAt(0));
			const decompressed = pako.inflate(compressed);
			return new TextDecoder().decode(decompressed);
		}

		const src = `# Paste your workflow YAML to this code editor

on:
  push:
    branch: main
    tags:
      - 'v\\d+'
jobs:
  test:
    strategy:
      matrix:
        os: [macos-latest, linux-latest]
    runs-on: \${{ matrix.os }}
    steps:
      - run: echo "Checking commit '\${{ github.event.head_commit.message }}'"
      - uses: actions/checkout@v7
      - uses: actions/setup-node@v7
        with:
          node_version: 18.x
      - uses: actions/cache@v6
        with:
          path: ~/.npm
          key: \${{ matrix.platform }}-node-\${{ hashFiles('**/package-lock.json') }}
        if: \${{ github.repository.permissions.admin == true }}
      - run: npm install && npm test`;

		return src;
	}

	const debounceInterval = isMobile().phone ? 1000 : 300;
	let debounceId: number | null = null;
	let contentChanged = false;

	function startActionlint(): void {
		debounceId = null;
		errorMessage.style.display = 'none';
		successMessage.style.display = 'none';
		invalidInputMessage.style.display = 'none';
		editor.dispatch(setDiagnostics(editor.state, []));
		// eslint-disable-next-line @typescript-eslint/no-non-null-assertion
		window.runActionlint!(getSource());
	}

	function onDocChanged(pasted: boolean): void {
		contentChanged = true;

		if (typeof window.runActionlint !== 'function') {
			showError('Preparing Wasm file is not completed yet. Please wait for a while and try again.');
			return;
		}

		if (debounceId !== null) {
			window.clearTimeout(debounceId);
		}

		if (pasted) {
			startActionlint(); // When pasting some code, apply actionlint instantly
			return;
		}

		debounceId = window.setTimeout(() => {
			startActionlint();
		}, debounceInterval);
	}

	const editor = new EditorView({
		doc: await getDefaultSource(),
		parent: getElementById('editor'),
		extensions: [
			basicSetup,
			yaml(),
			EditorView.lineWrapping,
			keymap.of([indentWithTab]),
			lintGutter(),
			editorTheme.of(themeFor(preferDark.matches)),
			EditorView.updateListener.of(update => {
				if (!update.docChanged) {
					return;
				}
				onDocChanged(update.transactions.some(tr => tr.isUserEvent('input.paste')));
			}),
		],
	});
	editor.focus();

	function getSource(): string {
		return editor.state.doc.toString();
	}

	function showError(message: string): void {
		errorMessage.textContent = message;
		errorMessage.style.display = 'block';
	}

	function showInvalidInputMessage(message: string): void {
		invalidInputMessage.textContent = message;
		invalidInputMessage.style.display = 'block';
		checkUrlInput.classList.add('is-danger');
	}

	function clearInvalidInputMessage(): void {
		checkUrlInput.classList.remove('is-danger');
		invalidInputMessage.style.display = 'none';
	}

	function dismissLoading(): void {
		nowLoading.style.display = 'none';
	}

	const reUrl = /https?:\/\/\S+/;
	function linkifyMessage(text: string): HTMLElement[] {
		function span(text: string): HTMLSpanElement {
			const e = document.createElement('span');
			e.textContent = text;
			return e;
		}

		const ret: HTMLElement[] = [];
		let rest = text;
		while (true) {
			const m = rest.match(reUrl);
			if (m === null || m.index === undefined) {
				if (rest.length > 0) {
					ret.push(span(rest));
				}
				return ret;
			}

			const idx = m.index;
			const url = m[0];

			const s = rest.slice(0, idx);
			if (s.length > 0) {
				ret.push(span(s));
			}

			const a = document.createElement('a');
			a.href = url;
			a.rel = 'noopener';
			a.textContent = url;
			a.className = 'has-text-link-my-light is-underlined';
			a.addEventListener('click', e => {
				e.stopPropagation();
			});
			ret.push(a);

			rest = rest.slice(idx + url.length);
		}
	}

	function onCheckCompleted(errors: ActionlintError[]): void {
		body.textContent = '';

		if (errors.length === 0) {
			successMessage.style.display = 'block';
			return;
		}

		const diagnostics: Diagnostic[] = [];

		for (const error of errors) {
			const row = document.createElement('tr');
			row.addEventListener('click', () => {
				editor.dispatch({
					selection: { anchor: errorRange(editor.state.doc, error).from },
					scrollIntoView: true,
				});
				editor.focus();
			});

			const pos = document.createElement('td');
			const tag = document.createElement('span');
			tag.className = 'tag is-dark is-medium';
			tag.textContent = `line:${error.line}, col:${error.column}`;
			pos.appendChild(tag);
			row.appendChild(pos);

			const desc = document.createElement('td');
			for (const elem of linkifyMessage(error.message)) {
				desc.appendChild(elem);
			}
			const kind = document.createElement('span');
			kind.className = 'tag is-dark';
			kind.textContent = error.kind;
			kind.style.marginLeft = '4px';
			desc.appendChild(kind);
			row.appendChild(desc);

			body.appendChild(row);

			const { from, to } = errorRange(editor.state.doc, error);
			diagnostics.push({
				from,
				to,
				severity: 'error',
				source: error.kind,
				message: error.message,
				renderMessage: () => {
					const wrapper = document.createElement('div');
					for (const elem of linkifyMessage(error.message)) {
						wrapper.appendChild(elem);
					}
					return wrapper;
				},
			});
		}

		editor.dispatch(setDiagnostics(editor.state, diagnostics));
	}

	window.getYamlSource = getSource;
	window.showError = showError;
	window.onCheckCompleted = onCheckCompleted;
	window.dismissLoading = dismissLoading;

	window.addEventListener('beforeunload', e => {
		if (contentChanged) {
			e.preventDefault();
		}
	});

	checkUrlInput.addEventListener('keyup', e => {
		if (e.key === 'Enter') {
			e.preventDefault();
			checkUrlButton.click();
		}
		if (checkUrlInput.value === '') {
			clearInvalidInputMessage();
		}
	});

	// eslint-disable-next-line @typescript-eslint/no-misused-promises
	checkUrlButton.addEventListener('click', async e => {
		e.preventDefault();
		const input = checkUrlInput.value;
		let src;
		try {
			src = await getRemoteSource(input);
		} catch (err) {
			if (!(err instanceof Error)) {
				throw err;
			}
			showInvalidInputMessage(`Incorrect input "${input}": ${err.message}`);
			return;
		}
		clearInvalidInputMessage();
		editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: src } });
	});

	permalinkButton.addEventListener('click', () => {
		void (async () => {
			const compressed = pako.deflate(new TextEncoder().encode(getSource()));
			window.location.hash = toBase64(compressed);
			const label = permalinkButton.textContent;
			try {
				await navigator.clipboard.writeText(window.location.href);
				permalinkButton.textContent = 'Copied!';
			} catch {
				permalinkButton.textContent = 'In address bar';
			}
			window.setTimeout(() => {
				permalinkButton.textContent = label;
			}, 1500);
		})();
	});

	preferDark.addEventListener('change', event => {
		editor.dispatch({ effects: editorTheme.reconfigure(themeFor(event.matches)) });
	});

	const go = new Go();

	let result;
	// Note: WebAssembly.instantiateStreaming is not implemented on Safari yet
	if (typeof WebAssembly.instantiateStreaming === 'function') {
		result = await WebAssembly.instantiateStreaming(fetch('main.wasm'), go.importObject);
	} else {
		const response = await fetch('main.wasm');
		const mod = await response.arrayBuffer();
		result = await WebAssembly.instantiate(mod, go.importObject);
	}

	await go.run(result.instance);
})().catch((err: unknown) => {
	console.error('ERROR!:', err);
	const msg = err instanceof Error ? `${err.name}: ${err.message}\n\n${err.stack}` : `Error: ${err}`;
	alert(msg);
});
