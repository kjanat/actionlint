const NS = {
	content: 'http://purl.org/rss/1.0/modules/content/',
	dc: 'http://purl.org/dc/elements/1.1/',
};

const el = {
	article: document.getElementById('article'),
	errorBox: document.getElementById('errorBox'),
	feedDescription: document.getElementById('feedDescription'),
	feedMeta: document.getElementById('feedMeta'),
	feedStats: document.getElementById('feedStats'),
	feedTitle: document.getElementById('feedTitle'),
	items: document.getElementById('items'),
	layout: document.querySelector('.layout'),
	raw: document.getElementById('raw'),
	rawBtn: document.getElementById('rawBtn'),
	refreshBtn: document.getElementById('refreshBtn'),
	search: document.getElementById('search'),
	typeFilter: document.getElementById('typeFilter'),
};

let state = {
	cacheComplete: false,
	feed: null,
	filtered: [],
	loadingAll: false,
	loadingOlder: false,
	nextPage: 2,
	pageSize: 10,
	rawVisible: false,
	rawXml: '',
	selectedId: null,
};

function syncPaneHeight() {
	if (!el.layout) return;

	if (window.matchMedia('(max-width: 850px)').matches) {
		el.layout.style.removeProperty('--pane-height');
		return;
	}

	const top = el.layout.getBoundingClientRect().top;
	const bottomGap = 12;
	const height = Math.max(
		320,
		Math.floor(window.innerHeight - top - bottomGap),
	);
	el.layout.style.setProperty('--pane-height', `${height}px`);
}

function schedulePaneHeightSync() {
	requestAnimationFrame(syncPaneHeight);
}

function text(parent, tag) {
	return parent?.getElementsByTagName(tag)?.[0]?.textContent?.trim()
		?? '';
}

function nsText(parent, ns, tag) {
	return parent?.getElementsByTagNameNS(ns, tag)?.[0]?.textContent
		?.trim() ?? '';
}

function parseDate(value) {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? null : date;
}

function formatDate(value) {
	const date = parseDate(value);
	if (!date) return value || 'Unknown date';
	return new Intl.DateTimeFormat(undefined, {
		dateStyle: 'medium',
		timeStyle: 'short',
	}).format(date);
}

function categories(item) {
	return [...item.getElementsByTagName('category')].map(node => ({
		domain: node.getAttribute('domain') || '',
		value: node.textContent.trim(),
	})).filter(x => x.value);
}

function sanitizeHtml(input) {
	const doc = new DOMParser().parseFromString(input || '', 'text/html');
	const blocked = new Set([
		'script',
		'style',
		'iframe',
		'object',
		'embed',
		'form',
		'input',
		'button',
		'textarea',
		'select',
		'meta',
		'base',
	]);

	[...doc.body.querySelectorAll('*')].forEach(node => {
		const tag = node.tagName.toLowerCase();
		if (blocked.has(tag)) {
			node.remove();
			return;
		}

		[...node.attributes].forEach(attr => {
			const name = attr.name.toLowerCase();
			const value = attr.value.trim();

			if (name.startsWith('on') || name === 'srcdoc') {
				node.removeAttribute(attr.name);
				return;
			}

			if (
				(name === 'href' || name === 'src' || name === 'poster')
				&& /^\s*(javascript|data:text\/html)/i.test(value)
			) {
				node.removeAttribute(attr.name);
			}
		});

		if (tag === 'a') {
			node.setAttribute('target', '_blank');
			node.setAttribute('rel', 'noopener noreferrer');
		}

		if (tag === 'video') {
			node.setAttribute('controls', '');
			node.removeAttribute('autoplay');
		}

		if (tag === 'img') {
			node.setAttribute('loading', 'lazy');
			node.setAttribute('decoding', 'async');
		}
	});

	return doc.body.innerHTML;
}

function formatHtmlFragment(htmlText, baseDepth = 0) {
	const doc = new DOMParser().parseFromString(
		htmlText || '',
		'text/html',
	);
	const lines = [];
	const voidTags = new Set([
		'area',
		'base',
		'br',
		'col',
		'embed',
		'hr',
		'img',
		'input',
		'link',
		'meta',
		'param',
		'source',
		'track',
		'wbr',
	]);

	function attrs(node) {
		return [...node.attributes].map(attr =>
			` ${attr.name}="${attr.value.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"`
		).join('');
	}

	function walk(node, depth) {
		const pad = '  '.repeat(depth);

		if (node.nodeType === Node.TEXT_NODE) {
			const value = node.nodeValue.replace(/\s+/g, ' ').trim();
			if (value) lines.push(pad + value);
			return;
		}

		if (node.nodeType === Node.COMMENT_NODE) {
			lines.push(`${pad}<!--${node.nodeValue}-->`);
			return;
		}

		if (node.nodeType !== Node.ELEMENT_NODE) return;

		const tag = node.tagName.toLowerCase();
		const opening = `<${tag}${attrs(node)}>`;
		if (voidTags.has(tag)) {
			lines.push(pad + opening);
			return;
		}

		const children = [...node.childNodes];
		const onlyText = children.length === 1
			&& children[0].nodeType === Node.TEXT_NODE;
		const textValue = onlyText
			? children[0].nodeValue.replace(/\s+/g, ' ').trim()
			: '';

		if (onlyText && textValue) {
			lines.push(`${pad}${opening}${textValue}</${tag}>`);
			return;
		}

		lines.push(pad + opening);
		for (const child of children) walk(child, depth + 1);
		lines.push(`${pad}</${tag}>`);
	}

	for (const child of [...doc.body.childNodes]) walk(child, baseDepth);
	return lines.join('\n');
}

function formatXml(xmlText) {
	const source = String(xmlText || '').trim();
	if (!source) return '';

	const doc = new DOMParser().parseFromString(
		source,
		'application/xml',
	);
	if (doc.querySelector('parsererror')) {
		// Fall back to the original source rather than mangling malformed XML.
		return source;
	}

	const lines = [];

	function xmlAttrs(node) {
		return [...node.attributes].map(attr =>
			` ${attr.name}="${attr.value.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"`
		).join('');
	}

	function walk(node, depth) {
		const pad = '  '.repeat(depth);

		if (node.nodeType === Node.PROCESSING_INSTRUCTION_NODE) {
			lines.push(`${pad}<?${node.nodeName} ${node.nodeValue}?>`);
			return;
		}

		if (node.nodeType === Node.COMMENT_NODE) {
			lines.push(`${pad}<!--${node.nodeValue}-->`);
			return;
		}

		if (node.nodeType === Node.CDATA_SECTION_NODE) {
			const value = node.nodeValue || '';
			const trimmed = value.trim();

			if (/<[A-Za-z][\s\S]*>/.test(trimmed)) {
				lines.push(`${pad}<![CDATA[`);
				const prettyHtml = formatHtmlFragment(trimmed, depth + 1);
				if (prettyHtml) lines.push(prettyHtml);
				lines.push(`${pad}]]>`);
			} else if (trimmed) {
				lines.push(`${pad}<![CDATA[${trimmed}]]>`);
			} else {
				lines.push(`${pad}<![CDATA[]]>`);
			}
			return;
		}

		if (node.nodeType === Node.TEXT_NODE) {
			const value = node.nodeValue.replace(/\s+/g, ' ').trim();
			if (value) lines.push(pad + value);
			return;
		}

		if (node.nodeType !== Node.ELEMENT_NODE) return;

		const name = node.tagName;
		const opening = `<${name}${xmlAttrs(node)}>`;
		const children = [...node.childNodes].filter(child => child.nodeType !== Node.TEXT_NODE || child.nodeValue.trim());

		if (!children.length) {
			lines.push(`${pad}<${name}${xmlAttrs(node)}/>`);
			return;
		}

		if (
			children.length === 1
			&& children[0].nodeType === Node.TEXT_NODE
		) {
			const value = children[0].nodeValue.replace(/\s+/g, ' ').trim();
			lines.push(`${pad}${opening}${value}</${name}>`);
			return;
		}

		if (
			children.length === 1
			&& children[0].nodeType === Node.CDATA_SECTION_NODE
			&& !/<[A-Za-z][\s\S]*>/.test(children[0].nodeValue.trim())
		) {
			lines.push(
				`${pad}${opening}<![CDATA[${children[0].nodeValue.trim()}]]></${name}>`,
			);
			return;
		}

		lines.push(pad + opening);
		for (const child of children) walk(child, depth + 1);
		lines.push(`${pad}</${name}>`);
	}

	if (doc.xmlVersion) {
		lines.push(
			`<?xml version="${doc.xmlVersion}" encoding="${doc.xmlEncoding || 'UTF-8'}"?>`,
		);
	}

	if (doc.doctype) {
		let d = `<!DOCTYPE ${doc.doctype.name}`;
		if (doc.doctype.publicId) d += ` PUBLIC "${doc.doctype.publicId}"`;
		if (doc.doctype.systemId) d += ` "${doc.doctype.systemId}"`;
		d += '>';
		lines.push(d);
	}

	walk(doc.documentElement, 0);
	return lines.join('\n');
}

function formatRawPages(rawXml) {
	const text = String(rawXml || '');
	const chunks = text.split(/(?=<!-- Feed page \d+ -->)/g).filter(
		chunk => chunk.trim(),
	);

	return chunks.map(chunk => {
		const match = chunk.match(
			/^\s*(<!-- Feed page \d+ -->)\s*([\s\S]*)$/,
		);
		if (!match) return formatXml(chunk);
		const [, header, xml] = match;
		return `${header}\n${formatXml(xml)}`;
	}).join('\n\n');
}

function parseFeed(xmlText) {
	const doc = new DOMParser().parseFromString(
		xmlText,
		'application/xml',
	);
	const parseError = doc.querySelector('parsererror');
	if (parseError) {
		throw new Error(parseError.textContent.replace(/\s+/g, ' ').trim());
	}

	const channel = doc.querySelector('rss > channel')
		|| doc.querySelector('channel');
	if (!channel) {
		throw new Error(
			'No RSS <channel> found. This viewer currently targets RSS 2.0 feeds.',
		);
	}

	const image = channel.querySelector(':scope > image');
	const itemNodes = [...channel.querySelectorAll(':scope > item')];

	const items = itemNodes.map((item, index) => {
		const cats = categories(item);
		const encoded = nsText(item, NS.content, 'encoded');
		const description = text(item, 'description');

		return {
			id: text(item, 'guid') || text(item, 'link') || String(index),
			title: text(item, 'title') || '(untitled)',
			link: text(item, 'link'),
			guid: text(item, 'guid'),
			author: nsText(item, NS.dc, 'creator') || text(item, 'author'),
			pubDate: text(item, 'pubDate'),
			description,
			content: encoded || description,
			categories: cats,
			types: cats.filter(x => x.domain === 'changelog-type').map(x => x.value),
			labels: cats.filter(x => x.domain === 'changelog-label').map(x => x.value),
		};
	});

	return {
		title: text(channel, 'title') || 'RSS feed',
		link: [...channel.children].find(x => x.tagName === 'link')?.textContent
			?.trim() || '',
		description: text(channel, 'description'),
		language: text(channel, 'language'),
		lastBuildDate: text(channel, 'lastBuildDate'),
		generator: text(channel, 'generator'),
		image: image ? text(image, 'url') : '',
		items,
	};
}

function unique(values) {
	return [...new Set(values.filter(Boolean))].sort((a, b) => a.localeCompare(b));
}

function fillSelect(select, values, label) {
	const current = select.value;
	select.replaceChildren(new Option(label, ''));
	values.forEach(value => select.add(new Option(value, value)));
	if (values.includes(current)) select.value = current;
}

function showError(message) {
	el.errorBox.hidden = false;
	el.errorBox.textContent = message;
}

function clearError() {
	el.errorBox.hidden = true;
	el.errorBox.textContent = '';
}

function loadXml(xmlText, sourceName = 'feed.xml') {
	try {
		const feed = parseFeed(xmlText);
		state = {
			rawXml: xmlText,
			feed,
			filtered: feed.items.slice(),
			selectedId: feed.items[0]?.id ?? null,
			rawVisible: false,
		};

		el.feedTitle.textContent = feed.title;
		el.feedDescription.textContent = feed.description;
		el.feedStats.textContent = `${feed.items.length} items`
			+ (feed.lastBuildDate
				? ` · built ${formatDate(feed.lastBuildDate)}`
				: '')
			+ ` · ${sourceName}`;
		el.feedMeta.classList.add('visible');
		schedulePaneHeightSync();

		fillSelect(
			el.typeFilter,
			unique(feed.items.flatMap(i => i.types)),
			'All types',
		);

		el.search.disabled = false;
		el.typeFilter.disabled = false;
		el.rawBtn.disabled = false;
		el.rawBtn.textContent = 'Raw XML';

		el.raw.textContent = formatXml(xmlText);
		el.raw.classList.remove('visible');
		el.article.style.display = '';
		applyFilters();
	} catch (error) {
		showError(error instanceof Error ? error.message : String(error));
	}
}

function itemSearchText(item) {
	const temp = document.createElement('div');
	temp.innerHTML = sanitizeHtml(item.content);
	return [
		item.title,
		item.author,
		item.pubDate,
		item.types.join(' '),
		item.labels.join(' '),
		temp.textContent || '',
	].join(' ').toLowerCase();
}

function applyFilters({ preserveArticle = false } = {}) {
	if (!state.feed) return;

	const query = el.search.value.trim().toLowerCase();
	const type = el.typeFilter.value;
	const previousSelectedId = state.selectedId;

	state.filtered = state.feed.items.filter(item => {
		if (type && !item.types.includes(type)) return false;
		if (query && !itemSearchText(item).includes(query)) return false;
		return true;
	});

	if (!state.filtered.some(item => item.id === state.selectedId)) {
		state.selectedId = state.filtered[0]?.id ?? null;
	}

	renderList();

	if (!preserveArticle || state.selectedId !== previousSelectedId) {
		renderArticle();
	}
}

function renderList() {
	const scrollTop = el.items.scrollTop;
	const anchor = [...el.items.querySelectorAll('.item')]
		.find(node => node.offsetTop + node.offsetHeight > scrollTop);
	const anchorId = anchor?.dataset.id ?? null;
	const anchorOffset = anchor ? anchor.offsetTop - scrollTop : 0;
	el.items.replaceChildren();

	if (!state.filtered.length) {
		const div = document.createElement('div');
		div.className = 'empty';
		div.textContent = 'No matching entries.';
		el.items.append(div);
		el.items.scrollTop = scrollTop;
		return;
	}

	const frag = document.createDocumentFragment();

	state.filtered.forEach(item => {
		const button = document.createElement('button');
		button.type = 'button';
		button.className = 'item'
			+ (item.id === state.selectedId ? ' active' : '');
		button.dataset.id = item.id;

		const title = document.createElement('span');
		title.className = 'item-title';
		title.textContent = item.title;

		const meta = document.createElement('span');
		meta.className = 'item-meta';

		const date = document.createElement('span');
		date.textContent = formatDate(item.pubDate);
		meta.append(date);

		[...item.types, ...item.labels].slice(0, 3).forEach(value => {
			const pill = document.createElement('span');
			pill.className = 'pill';
			pill.textContent = value;
			meta.append(pill);
		});

		button.append(title, meta);
		button.addEventListener('click', () => {
			state.selectedId = item.id;
			renderList();
			renderArticle();
		});

		frag.append(button);
	});

	el.items.append(frag);
	const nextAnchor = anchorId
		? [...el.items.querySelectorAll('.item')].find(node => node.dataset.id === anchorId)
		: null;
	el.items.scrollTop = nextAnchor
		? nextAnchor.offsetTop - anchorOffset
		: scrollTop;
}

function renderArticle() {
	if (state.rawVisible || !state.feed) return;

	const item = state.feed.items.find(i => i.id === state.selectedId);
	if (!item) {
		el.article.className = 'empty';
		el.article.innerHTML = 'No entry selected.';
		return;
	}

	const safe = sanitizeHtml(item.content);

	el.article.className = '';
	el.article.innerHTML = `
          <header class="article-head">
            <h2></h2>
            <div class="meta"></div>
            <div class="article-actions"></div>
          </header>
          <article class="article-content"></article>
        `;

	el.article.querySelector('h2').textContent = item.title;

	const meta = el.article.querySelector('.meta');
	const parts = [
		item.author ? `By ${item.author}` : '',
		item.pubDate ? formatDate(item.pubDate) : '',
	].filter(Boolean);

	parts.forEach(part => {
		const span = document.createElement('span');
		span.textContent = part;
		meta.append(span);
	});

	item.categories.forEach(category => {
		const span = document.createElement('span');
		span.className = 'pill';
		span.textContent = category.value;
		if (category.domain) span.title = category.domain;
		meta.append(span);
	});

	const actions = el.article.querySelector('.article-actions');
	if (item.link) {
		const link = document.createElement('a');
		link.href = item.link;
		link.target = '_blank';
		link.rel = 'noopener noreferrer';
		link.textContent = 'Open original ↗';
		actions.append(link);
	}

	const content = el.article.querySelector('.article-content');
	content.innerHTML = safe || '<p>No content in this entry.</p>';
}

const FEED_URL = 'https://github.blog/changelog/label/actions/feed/';
const MAX_PAGES = 50;
const DB_NAME = 'github-actions-changelog-reader';
const DB_VERSION = 1;
const STORE_NAME = 'feeds';

function sortItems(items) {
	items.sort((a, b) => {
		const aTime = parseDate(a.pubDate)?.getTime() ?? 0;
		const bTime = parseDate(b.pubDate)?.getTime() ?? 0;
		return bTime - aTime;
	});
}

function updateFeedUi({ preserveArticle = false } = {}) {
	if (!state.feed) return;

	el.feedTitle.textContent = state.feed.title;
	el.feedDescription.textContent = state.feed.description;
	el.feedStats.textContent = `${state.feed.items.length} entries`
		+ (state.cacheComplete ? '' : ' loaded')
		+ (state.loadingAll ? ' · searching history…' : '')
		+ (state.feed.lastBuildDate
			? ` · built ${formatDate(state.feed.lastBuildDate)}`
			: '');
	el.feedMeta.classList.add('visible');
	schedulePaneHeightSync();

	fillSelect(
		el.typeFilter,
		unique(state.feed.items.flatMap(i => i.types)),
		'All types',
	);
	el.search.disabled = false;
	el.typeFilter.disabled = false;
	el.rawBtn.disabled = false;

	el.raw.textContent = formatRawPages(state.rawXml);
	applyFilters({ preserveArticle });
}

function hydrateFeed(feed, rawXml, {
	complete = false,
	preserveArticle = false,
	nextPage = 2,
} = {}) {
	const selectedId = state.selectedId;
	sortItems(feed.items);
	state.feed = feed;
	state.rawXml = rawXml || '';
	state.filtered = feed.items.slice();
	state.cacheComplete = complete;
	state.nextPage = Math.max(2, nextPage || 2);
	state.pageSize = 10;
	state.selectedId = selectedId && feed.items.some(item => item.id === selectedId)
		? selectedId
		: (feed.items[0]?.id ?? null);
	updateFeedUi({ preserveArticle });
}

function overlayItems(feedPage, { preserveArticle = true } = {}) {
	if (!state.feed) {
		hydrateFeed({ ...feedPage, items: [...feedPage.items] }, '', {
			preserveArticle: false,
		});
		return { added: feedPage.items.length, updated: 0 };
	}

	const byId = new Map(state.feed.items.map(item => [item.id, item]));
	let added = 0;
	let updated = 0;

	for (const item of feedPage.items) {
		const previous = byId.get(item.id);
		if (!previous) {
			state.feed.items.push(item);
			byId.set(item.id, item);
			added++;
			continue;
		}

		const index = state.feed.items.indexOf(previous);
		if (index >= 0) {
			state.feed.items[index] = item;
			byId.set(item.id, item);
			updated++;
		}
	}

	if (feedPage.title) state.feed.title = feedPage.title;
	if (feedPage.description) {
		state.feed.description = feedPage.description;
	}
	if (feedPage.lastBuildDate) {
		state.feed.lastBuildDate = feedPage.lastBuildDate;
	}

	sortItems(state.feed.items);
	updateFeedUi({ preserveArticle });
	return { added, updated };
}

function openDb() {
	return new Promise((resolve, reject) => {
		if (!('indexedDB' in window)) {
			reject(new Error('IndexedDB is unavailable'));
			return;
		}

		const request = indexedDB.open(DB_NAME, DB_VERSION);
		request.onerror = () => reject(request.error);
		request.onupgradeneeded = () => {
			const db = request.result;
			if (!db.objectStoreNames.contains(STORE_NAME)) {
				db.createObjectStore(STORE_NAME);
			}
		};
		request.onsuccess = () => resolve(request.result);
	});
}

async function readCache() {
	try {
		const db = await openDb();
		return await new Promise((resolve, reject) => {
			const tx = db.transaction(STORE_NAME, 'readonly');
			const request = tx.objectStore(STORE_NAME).get(FEED_URL);
			request.onerror = () => reject(request.error);
			request.onsuccess = () => resolve(request.result ?? null);
			tx.oncomplete = () => db.close();
		});
	} catch {
		return null;
	}
}

async function writeCache(complete = state.cacheComplete) {
	if (!state.feed) return;
	try {
		const db = await openDb();
		await new Promise((resolve, reject) => {
			const tx = db.transaction(STORE_NAME, 'readwrite');
			tx.objectStore(STORE_NAME).put({
				version: 2,
				savedAt: Date.now(),
				complete,
				nextPage: state.nextPage,
				rawXml: state.rawXml,
				feed: state.feed,
			}, FEED_URL);
			tx.onerror = () => reject(tx.error);
			tx.oncomplete = resolve;
		});
		db.close();
	} catch {
		// Caching is an optimization. Reading the feed must keep working without it.
	}
}

async function fetchFeedPage(page) {
	const url = new URL(FEED_URL);
	if (page > 1) url.searchParams.set('paged', String(page));

	const response = await fetch(url.href, {
		cache: 'no-store',
		headers: {
			'Accept': 'application/rss+xml, application/xml, text/xml, */*',
		},
	});

	if (!response.ok) {
		if (page > 1 && response.status === 404) return null;
		throw new Error(`HTTP ${response.status} ${response.statusText}`);
	}

	const xmlText = await response.text();
	return { url: url.href, xmlText, feed: parseFeed(xmlText) };
}

function nextFrame() {
	return new Promise(resolve => requestAnimationFrame(() => resolve()));
}

function hasActiveHistoryQuery() {
	return Boolean(el.search.value.trim() || el.typeFilter.value);
}

async function loadHistoricalPage(page = state.nextPage) {
	if (state.cacheComplete || state.loadingOlder) return false;

	state.loadingOlder = true;
	try {
		const result = await fetchFeedPage(page);
		if (!result || result.feed.items.length === 0) {
			state.cacheComplete = true;
			updateFeedUi({ preserveArticle: true });
			void writeCache(true);
			return false;
		}

		const previousPageSize = state.pageSize;
		overlayItems(result.feed, { preserveArticle: true });
		state.pageSize = Math.max(1, result.feed.items.length);
		state.nextPage = Math.max(state.nextPage, page + 1);

		// Raw XML is diagnostic only; keep the pages that were actually fetched this session.
		state.rawXml += `${state.rawXml ? '\n\n' : ''}<!-- Feed page ${page} -->\n${result.xmlText}`;
		el.raw.textContent = formatRawPages(state.rawXml);

		if (
			result.feed.items.length < previousPageSize
			|| result.feed.items.length === 0
		) {
			state.cacheComplete = true;
		}

		void writeCache(state.cacheComplete);
		return true;
	} finally {
		state.loadingOlder = false;
	}
}

async function ensureScrollable() {
	if (!state.feed || state.cacheComplete || hasActiveHistoryQuery()) {
		return;
	}

	// Rendered dimensions are only trustworthy after layout.
	await nextFrame();

	let attempts = 0;
	while (
		!state.cacheComplete
		&& !hasActiveHistoryQuery()
		&& el.items.scrollHeight <= el.items.clientHeight + 2
		&& attempts++ < MAX_PAGES
	) {
		const loaded = await loadHistoricalPage(state.nextPage);
		if (!loaded) break;
		await nextFrame();
	}
}

async function maybeLoadNearBottom() {
	if (
		!state.feed || state.cacheComplete || state.loadingOlder
		|| hasActiveHistoryQuery()
	) return;

	const remaining = el.items.scrollHeight - el.items.scrollTop
		- el.items.clientHeight;
	if (remaining > 320) return;

	const loaded = await loadHistoricalPage(state.nextPage);
	if (!loaded) return;
	await nextFrame();

	// If one page still leaves us at the bottom, continue. This also intentionally
	// crosses pages that contain only cached overlap after feed pagination shifted.
	const nextRemaining = el.items.scrollHeight - el.items.scrollTop
		- el.items.clientHeight;
	if (!state.cacheComplete && nextRemaining <= 120) {
		setTimeout(() => void maybeLoadNearBottom(), 0);
	}
}

async function loadAllHistory() {
	if (!state.feed || state.cacheComplete || state.loadingAll) return;

	state.loadingAll = true;
	updateFeedUi({ preserveArticle: true });

	try {
		while (!state.cacheComplete && state.nextPage <= MAX_PAGES) {
			const page = state.nextPage;
			const result = await fetchFeedPage(page);

			if (!result || result.feed.items.length === 0) {
				state.cacheComplete = true;
				break;
			}

			const previousPageSize = state.pageSize;
			overlayItems(result.feed, { preserveArticle: true });
			state.pageSize = Math.max(1, result.feed.items.length);
			state.nextPage = page + 1;

			state.rawXml += `${state.rawXml ? '\n\n' : ''}<!-- Feed page ${page} -->\n${result.xmlText}`;
			el.raw.textContent = formatRawPages(state.rawXml);

			if (result.feed.items.length < previousPageSize) {
				state.cacheComplete = true;
				break;
			}
		}

		if (state.nextPage > MAX_PAGES) state.cacheComplete = true;
		await writeCache(state.cacheComplete);
	} catch (error) {
		const detail = error instanceof Error
			? error.message
			: String(error);
		showError(`Could not load older changelog history: ${detail}`);
	} finally {
		state.loadingAll = false;
		updateFeedUi({ preserveArticle: true });
	}
}

async function refreshLatest() {
	const previous = el.refreshBtn.textContent;
	el.refreshBtn.disabled = true;
	el.refreshBtn.textContent = 'Refreshing…';
	clearError();

	const hadFeed = Boolean(state.feed);
	const knownBefore = new Set(
		state.feed?.items.map(item => item.id) ?? [],
	);

	try {
		const first = await fetchFeedPage(1);
		if (!first) throw new Error('The feed returned no first page.');

		const firstOverlapsCache = first.feed.items.some(item => knownBefore.has(item.id));

		if (!state.feed) {
			hydrateFeed(
				{ ...first.feed, items: [...first.feed.items] },
				first.xmlText,
				{
					complete: false,
					preserveArticle: false,
					nextPage: 2,
				},
			);
		} else {
			overlayItems(first.feed, { preserveArticle: true });
			state.rawXml = `<!-- Feed page 1 -->\n${first.xmlText}`;
			el.raw.textContent = formatRawPages(state.rawXml);
		}

		state.pageSize = Math.max(1, first.feed.items.length);
		state.nextPage = Math.max(2, state.nextPage);

		/*
		 * Normal refresh ends here.
		 *
		 * Only if page 1 contains *no* previously known item do we know that >= one
		 * complete page of new entries arrived since our cache. In that case fetch just
		 * enough following pages to reconnect with the cached history, so no new entry
		 * can fall through the page boundary.
		 */
		if (hadFeed && knownBefore.size && !firstOverlapsCache) {
			for (let page = 2; page <= MAX_PAGES; page++) {
				const result = await fetchFeedPage(page);
				if (!result || result.feed.items.length === 0) {
					state.cacheComplete = true;
					break;
				}

				const overlapsCache = result.feed.items.some(item => knownBefore.has(item.id));
				overlayItems(result.feed, { preserveArticle: true });
				state.nextPage = Math.max(state.nextPage, page + 1);

				state.rawXml += `\n\n<!-- Feed page ${page} -->\n${result.xmlText}`;
				el.raw.textContent = formatRawPages(state.rawXml);

				if (
					overlapsCache || result.feed.items.length < state.pageSize
				) break;
			}
		}

		void writeCache(state.cacheComplete);

		// On a cold start, load only enough old history to make the list genuinely scrollable.
		// If cached content already provides a scrollbar this resolves without another request.
		void ensureScrollable();
	} catch (error) {
		const detail = error instanceof Error
			? error.message
			: String(error);
		if (!state.feed) {
			showError(
				`Could not load the GitHub Actions feed: ${detail}. `
					+ 'If this is a browser CORS failure, serve the HTML over HTTP rather than opening it as file://.',
			);
		} else {
			showError(`Could not refresh newer changelog entries: ${detail}`);
		}
	} finally {
		el.refreshBtn.disabled = false;
		el.refreshBtn.textContent = previous;
	}
}

function start() {
	/*
	 * Stale-while-revalidate:
	 *   1. IndexedDB may paint immediately.
	 *   2. Page 1 is independently refreshed from the network.
	 *   3. Historical pages stay lazy until the list needs a scrollbar, the user
	 *      scrolls near the bottom, or a search/filter needs the full history.
	 */
	void readCache().then(cached => {
		if (!state.feed && cached?.feed?.items?.length) {
			const inferredNextPage = Number.isInteger(cached.nextPage)
				? cached.nextPage
				: Math.max(2, Math.floor(cached.feed.items.length / 10) + 1);

			hydrateFeed(cached.feed, cached.rawXml, {
				complete: Boolean(cached.complete),
				preserveArticle: false,
				nextPage: inferredNextPage,
			});
			void ensureScrollable();
		}
	});

	void refreshLatest();
}

el.refreshBtn.addEventListener('click', () => void refreshLatest());

let searchTimer = null;
el.search.addEventListener('input', () => {
	applyFilters();
	clearTimeout(searchTimer);

	if (el.search.value.trim()) {
		searchTimer = setTimeout(() => void loadAllHistory(), 120);
	} else {
		void ensureScrollable();
	}
});

el.typeFilter.addEventListener('change', () => {
	applyFilters();
	if (el.typeFilter.value) {
		void loadAllHistory();
	} else {
		void ensureScrollable();
	}
});

el.items.addEventListener('scroll', () => void maybeLoadNearBottom(), {
	passive: true,
});

window.addEventListener('resize', schedulePaneHeightSync, {
	passive: true,
});

el.rawBtn.addEventListener('click', () => {
	if (!state.feed) return;
	state.rawVisible = !state.rawVisible;
	el.raw.classList.toggle('visible', state.rawVisible);
	el.article.style.display = state.rawVisible ? 'none' : '';
	el.rawBtn.textContent = state.rawVisible
		? 'Rendered view'
		: 'Raw XML';
	if (!state.rawVisible) renderArticle();
	schedulePaneHeightSync();
});

schedulePaneHeightSync();
start();
