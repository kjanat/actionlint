import { defaultKeymap } from '@codemirror/commands';
import { xml } from '@codemirror/lang-xml';
import {
	ensureSyntaxTree,
	foldAll,
	foldGutter,
	foldKeymap,
	HighlightStyle,
	syntaxHighlighting,
	unfoldAll,
} from '@codemirror/language';
import { openSearchPanel, search, searchKeymap } from '@codemirror/search';
import { EditorState } from '@codemirror/state';
import { drawSelection, EditorView, keymap, lineNumbers } from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { default as formatXml } from 'xml-formatter';
import { parseFeed } from './github-actions-feed.ts';
import { readRawPages } from './reader-pages.ts';

export function createXmlViewer(container: HTMLElement, loadOlder: () => Promise<boolean>) {
	const toolbar = document.createElement('div');
	toolbar.className = 'xml-toolbar';
	const label = document.createElement('label');
	label.textContent = 'Feed page ';
	const select = document.createElement('select');
	select.setAttribute('aria-label', 'Feed page');
	label.append(select);
	toolbar.append(label);
	const entries = document.createElement('select');
	entries.setAttribute('aria-label', 'Jump to article in XML');
	entries.className = 'xml-entries';
	toolbar.append(entries);
	const editor = document.createElement('div');
	editor.className = 'xml-editor';
	container.replaceChildren(toolbar, editor);
	const view = new EditorView({
		parent: editor,
		extensions: [
			xml({ autoCloseTags: false }),
			EditorState.readOnly.of(true),
			EditorView.editable.of(false),
			EditorView.contentAttributes.of({ tabindex: '0', 'aria-label': 'Feed XML' }),
			lineNumbers(),
			drawSelection(),
			foldGutter(),
			search({ top: true }),
			keymap.of([...searchKeymap, ...foldKeymap, ...defaultKeymap]),
			syntaxHighlighting(HighlightStyle.define([
				{ tag: tags.tagName, color: 'var(--xml-tag)' },
				{ tag: tags.attributeName, color: 'var(--xml-attribute)' },
				{ tag: tags.string, color: 'var(--xml-string)' },
				{ tag: tags.comment, color: 'var(--muted)' },
				{ tag: tags.processingInstruction, color: 'var(--xml-attribute)' },
			])),
			EditorView.theme({
				'&': { height: '100%', color: 'var(--text)', backgroundColor: 'var(--panel-2)' },
				'.cm-scroller': { overflow: 'auto' },
				'.cm-gutters, .cm-panels': {
					backgroundColor: 'var(--panel)',
					color: 'var(--muted)',
					borderColor: 'var(--border)',
				},
				'.cm-content': { padding: '12px 0' },
				'.cm-line': { padding: '0 12px' },
				'&.cm-focused': { outline: '2px solid var(--accent)', outlineOffset: '-2px' },
			}),
		],
	});
	let pages = new Map<number, string>();
	let shown = '';
	function showPage() {
		const source = pages.get(Number(select.value)) ?? '';
		if (source === shown) return;
		shown = source;
		let doc = source;
		try {
			const format = typeof formatXml === 'function' ? formatXml : formatXml.default;
			doc = format(source, { indentation: '  ', collapseContent: true, lineSeparator: '\n' });
		} catch {
			// Keep malformed XML available for inspection.
		}
		view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: doc }, selection: { anchor: 0 } });
		view.scrollDOM.scrollTop = 0;
		entries.replaceChildren(new Option('Jump to article…', ''));
		const items = parseFeed(source).items;
		let itemIndex = 0;
		ensureSyntaxTree(view.state, doc.length, 100)?.iterate({
			enter(node) {
				const tag = node.node.getChild('OpenTag')?.getChild('TagName');
				if (node.name !== 'Element' || !tag || doc.slice(tag.from, tag.to) !== 'item') return;
				const item = items[itemIndex++];
				entries.add(new Option(item?.title ?? `Article ${itemIndex}`, String(node.from)));
			},
		});
	}
	entries.addEventListener('change', () => {
		if (!entries.value) return;
		unfoldAll(view);
		view.dispatch({ selection: { anchor: Number(entries.value) }, scrollIntoView: true });
		view.focus();
	});
	select.addEventListener('change', showPage);
	function button(text: string, action: () => void) {
		const button = document.createElement('button');
		button.type = 'button';
		button.textContent = text;
		button.addEventListener('click', action);
		toolbar.append(button);
		return button;
	}
	const previous = button('Previous page', () => {
		select.selectedIndex--;
		showPage();
		updateButtons();
	});
	const next = button('Next page', () => {
		select.selectedIndex++;
		showPage();
		updateButtons();
	});
	select.addEventListener('change', updateButtons);
	const load = button('Load older page', () => {
		load.disabled = true;
		void loadOlder().then(loaded => {
			if (loaded) {
				select.selectedIndex = select.length - 1;
				showPage();
				updateButtons();
			}
		}).catch(() => {
			status.textContent = 'Could not load an older page. Try again.';
		}).finally(() => {
			load.disabled = complete;
		});
	});
	const status = document.createElement('span');
	status.setAttribute('role', 'status');
	toolbar.append(status);
	let complete = false;
	button('Collapse all', () => {
		foldAll(view);
	});
	button('Expand all', () => {
		unfoldAll(view);
	});
	button('Find in XML', () => {
		openSearchPanel(view);
	});
	function updateButtons() {
		previous.disabled = select.selectedIndex <= 0;
		next.disabled = select.selectedIndex >= select.length - 1;
	}
	return {
		update(rawXml: string, historyComplete: boolean) {
			complete = historyComplete;
			load.disabled = complete;
			status.textContent = '';
			pages = readRawPages(rawXml);
			const selected = select.value;
			select.replaceChildren(
				...[...pages.keys()].sort((a, b) => a - b).map(page => new Option(String(page), String(page))),
			);
			if (pages.has(Number(selected))) select.value = selected;
			showPage();
			updateButtons();
		},
	};
}
