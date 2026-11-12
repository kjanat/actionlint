import 'bulma/css/bulma.min.css';
import 'devicon/devicon.min.css';
import './style.css';

import { autocompletion, type CompletionContext, type CompletionResult } from '@codemirror/autocomplete';
import { indentWithTab } from '@codemirror/commands';
import { yaml } from '@codemirror/lang-yaml';
import type { Diagnostic } from '@codemirror/lint';
import { lintGutter, setDiagnostics } from '@codemirror/lint';
import type { Extension } from '@codemirror/state';
import { Compartment } from '@codemirror/state';
import { oneDark } from '@codemirror/theme-one-dark';
import { EditorView, keymap } from '@codemirror/view';
import type {
	CodeAction,
	Diagnostic as LSPDiagnostic,
	PublishDiagnosticsParams,
} from '@kjanat-actionlint/language-server';
import { basicSetup } from 'codemirror';

import { documentURI, type PlaygroundClient, startPlaygroundClient } from '#client';
import { editorRange, workspaceChanges } from '#editor-lsp';
import { shellcheckAssets } from '#shellcheck';
import { querySource, readSource, sharedSource, shareURL } from '#source';

const editorTheme = new Compartment();

function themeFor(isDark: boolean): Extension {
	return isDark ? oneDark : [];
}

(async () => {
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
	const checkUrlInput = getElementById('check-url-input');
	if (!(checkUrlInput instanceof HTMLInputElement)) throw new Error('The source URL field must be an input');
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
		const src = await readSource(res);
		return src.trim();
	}

	async function getDefaultSource(): Promise<string> {
		const params = new URLSearchParams(window.location.search);

		const s = params.get('s');
		if (s !== null) {
			return querySource(s);
		}

		const u = params.get('u');
		if (u !== null) {
			return getRemoteSource(u);
		}

		if (window.location.hash !== '') {
			return sharedSource(window.location.hash.slice(1));
		}

		const src = `\
# Paste your workflow YAML to this code editor
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
    shell: bash
    steps:
      - run: echo "Checking commit '\${{ github.event.head_commit.message }}'"
      - uses: actions/checkout@v3
      - uses: actions/setup-node@v7
        with: { node_version: 18.x }
      - uses: actions/cache@v6
        with:
          path: ~/.npm
          key: \${{ matrix.platform }}-node-\${{ hashFiles('**/package-lock.json') }}
        if: \${{ github.repository.permissions.admin == true }}
      - run: npm install && npm test && echo $globberdyglobberdyglob && echo '$huh'
`;

		return src;
	}

	let contentChanged = false;
	const session: { client?: PlaygroundClient } = {};
	let version = 1;
	let failedVersion: number | undefined;

	function clearResults(): void {
		body.textContent = '';
		errorMessage.style.display = 'none';
		successMessage.style.display = 'none';
		editor.dispatch(setDiagnostics(editor.state, []));
	}

	function onDocChanged(): void {
		contentChanged = true;
		version++;
		failedVersion = undefined;
		clearResults();
		invalidInputMessage.style.display = 'none';
		if (session.client) void session.client.change(documentURI, getSource(), version).catch(showFailure);
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
			autocompletion({ override: [complete] }),
			editorTheme.of(themeFor(preferDark.matches)),
			EditorView.updateListener.of(update => {
				if (!update.docChanged) {
					return;
				}
				onDocChanged();
			}),
		],
	});
	editor.focus();

	function getSource(): string {
		return editor.state.doc.toString();
	}

	function showError(message: string): void {
		failedVersion = version;
		clearResults();
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

	function showFailure(error: unknown): void {
		showError(error instanceof Error ? error.message : String(error));
	}

	function sameFinding(left: LSPDiagnostic, right: LSPDiagnostic): boolean {
		return left.code === right.code && JSON.stringify(left.range) === JSON.stringify(right.range);
	}

	function applyAction(action: CodeAction, expectedVersion: number): void {
		if (version !== expectedVersion || !action.edit) return;
		const changes = workspaceChanges(editor.state.doc, action.edit, documentURI, version);
		if (!changes) {
			showError('This fix requires another document or a different document version.');
			return;
		}
		editor.dispatch({ changes });
	}

	async function onDiagnostics(params: PublishDiagnosticsParams): Promise<void> {
		if (params.uri !== documentURI || params.version !== version) return;
		const failure = failedVersion === version ? errorMessage.textContent : null;
		clearResults();
		if (failure) {
			errorMessage.style.display = 'block';
			errorMessage.textContent = failure;
		}
		if (params.diagnostics.length === 0) {
			if (!failure) successMessage.style.display = 'block';
			return;
		}
		const snapshot = version;
		const last = editor.state.doc.line(editor.state.doc.lines);
		const actions = await session.client?.codeActions({
			textDocument: { uri: documentURI },
			range: { start: { line: 0, character: 0 }, end: { line: editor.state.doc.lines - 1, character: last.length } },
			context: { diagnostics: params.diagnostics, only: ['quickfix'] },
		}).catch((error: unknown) => {
			if (version === snapshot) throw error;
			return [];
		});
		if (version !== snapshot) return;
		const diagnostics: Diagnostic[] = [];
		for (const finding of params.diagnostics) {
			const message = typeof finding.message === 'string' ? finding.message : finding.message.value;
			const fixes = (actions ?? []).filter((action): action is CodeAction =>
				'edit' in action && action.edit !== undefined
				&& (action.diagnostics?.some(d => sameFinding(d, finding)) ?? false)
			);
			const { from, to } = editorRange(editor.state.doc, finding.range);
			const severity = finding.severity === 2
				? 'warning'
				: finding.severity === 3
				? 'info'
				: finding.severity === 4
				? 'hint'
				: 'error';
			const row = document.createElement('tr');
			row.dataset.severity = severity;
			row.addEventListener('click', () => {
				editor.dispatch({ selection: { anchor: from }, scrollIntoView: true });
				editor.focus();
			});
			const pos = document.createElement('td');
			const tag = document.createElement('span');
			tag.className = `tag is-medium ${
				severity === 'error' ? 'is-danger' : severity === 'warning' ? 'is-warning' : 'is-info'
			}`;
			tag.textContent = `${severity} · ${finding.range.start.line + 1}:${finding.range.start.character + 1}`;
			pos.append(tag);
			const desc = document.createElement('td');
			desc.append(...linkifyMessage(message));
			const code = document.createElement('span');
			code.className = 'tag is-dark';
			code.textContent = String(finding.code ?? finding.source ?? 'actionlint');
			desc.append(code);
			if (finding.codeDescription) {
				const docs = document.createElement('a');
				docs.href = finding.codeDescription.href;
				docs.textContent = 'Documentation';
				docs.target = '_blank';
				docs.rel = 'noopener noreferrer';
				desc.append(docs);
			}
			for (const related of finding.relatedInformation ?? []) {
				const item = document.createElement('div');
				item.textContent = `${related.message} (${related.location.uri}:${related.location.range.start.line + 1})`;
				desc.append(item);
			}
			for (const fix of fixes) {
				const button = document.createElement('button');
				button.type = 'button';
				button.className = 'button is-small';
				button.textContent = fix.title;
				button.addEventListener('click', event => {
					event.stopPropagation();
					applyAction(fix, snapshot);
				});
				desc.append(button);
			}
			row.append(pos, desc);
			body.append(row);
			diagnostics.push({
				from,
				to,
				severity,
				source: String(finding.code ?? finding.source ?? 'actionlint'),
				message,
				actions: fixes.map(fix => ({
					name: fix.title,
					apply: () => {
						applyAction(fix, snapshot);
					},
				})),
			});
		}
		editor.dispatch(setDiagnostics(editor.state, diagnostics));
	}

	async function complete(context: CompletionContext): Promise<CompletionResult | null> {
		if (!session.client) return null;
		const snapshot = version;
		const line = context.state.doc.lineAt(context.pos);
		const result = await session.client.complete({
			textDocument: { uri: documentURI },
			position: { line: line.number - 1, character: context.pos - line.from },
		});
		if (!result || snapshot !== version || context.aborted) return null;
		const items = Array.isArray(result) ? result : result.items;
		return {
			from: context.matchBefore(/[\w-]*$/)?.from ?? context.pos,
			options: items.map(item => ({
				label: item.label,
				detail: item.detail,
				apply(view, _completion, from, to) {
					if (version !== snapshot) return;
					const edit = item.textEdit;
					const range = edit ? editorRange(view.state.doc, 'range' in edit ? edit.range : edit.replace) : { from, to };
					view.dispatch({ changes: { ...range, insert: edit?.newText ?? item.insertText ?? item.label } });
				},
			})),
		};
	}

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

	checkUrlButton.addEventListener('click', e => {
		void (async () => {
			e.preventDefault();
			const input = checkUrlInput.value;
			let src: string;
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
		})().catch(showFailure);
	});

	permalinkButton.addEventListener('click', () => {
		void (async () => {
			const label = permalinkButton.textContent;
			let url: string | undefined;
			try {
				url = shareURL(window.location.origin + window.location.pathname, getSource());
			} catch {
				permalinkButton.textContent = 'Too large to share';
			}
			if (url !== undefined) {
				window.history.replaceState(null, '', url);
				try {
					await navigator.clipboard.writeText(url);
					permalinkButton.textContent = 'Copied!';
				} catch {
					permalinkButton.textContent = 'In address bar';
				}
			}
			window.setTimeout(() => {
				permalinkButton.textContent = label;
			}, 1500);
		})();
	});

	preferDark.addEventListener('change', event => {
		editor.dispatch({ effects: editorTheme.reconfigure(themeFor(event.matches)) });
	});

	getElementById('shellcheck-license').setAttribute('href', shellcheckAssets.license);
	session.client = await startPlaygroundClient({
		onDiagnostics: params => {
			void onDiagnostics(params).catch(showFailure);
		},
		onError: showError,
	});
	await session.client.open(documentURI, getSource(), version);
	dismissLoading();
})().catch((err: unknown) => {
	console.error('ERROR!:', err);
	const msg = err instanceof Error ? err.message : String(err);
	const loading = document.getElementById('loading');
	if (loading) loading.style.display = 'none';
	const error = document.getElementById('error-msg');
	if (error) {
		error.textContent = msg;
		error.style.display = 'block';
	}
});
