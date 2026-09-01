import { FEED_URL, parseFeed } from './github-actions-feed.js';

/** @typedef {import('#feed').Feed} Feed */
/** @typedef {import('#feed').FeedItem} FeedItem */

/**
 * @typedef CachedFeed
 * @property {boolean} complete
 * @property {Feed} feed
 * @property {number} nextPage
 * @property {string} rawXml
 */

/**
 * @template {keyof HTMLElementTagNameMap} K
 * @param {string} id
 * @param {K} tagName
 * @returns {HTMLElementTagNameMap[K]}
 */
function requireElement(id, tagName) {
	const node = document.getElementById(id);
	if (!node || node.localName !== tagName) {
		throw new Error(`Expected <${tagName} id="${id}">.`);
	}
	return /** @type {HTMLElementTagNameMap[K]} */ (node);
}

const el = {
	article: requireElement('article', 'div'),
	errorBox: requireElement('errorBox', 'div'),
	feedDescription: requireElement('feedDescription', 'div'),
	feedMeta: requireElement('feedMeta', 'section'),
	feedStats: requireElement('feedStats', 'div'),
	feedTitle: requireElement('feedTitle', 'div'),
	items: requireElement('items', 'div'),
	layout: /** @type {HTMLDivElement | null} */ (document.querySelector('.layout')),
	raw: requireElement('raw', 'pre'),
	rawBtn: requireElement('rawBtn', 'button'),
	refreshBtn: requireElement('refreshBtn', 'button'),
	search: requireElement('search', 'input'),
	typeFilter: requireElement('typeFilter', 'select'),
};

/**
 * @type {{
 *   cacheComplete: boolean,
 *   feed: Feed | null,
 *   filtered: FeedItem[],
 *   loadingAll: boolean,
 *   loadingOlder: boolean,
 *   nextPage: number,
 *   pageSize: number,
 *   rawVisible: boolean,
 *   rawXml: string,
 *   selectedId: string | null,
 * }}
 */
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

/** @param {string} value */
function parseDate(value) {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? null : date;
}

/** @param {string} value */
function formatDate(value) {
	const date = parseDate(value);
	if (!date) return value || 'Unknown date';
	return new Intl.DateTimeFormat(undefined, {
		dateStyle: 'medium',
		timeStyle: 'short',
	}).format(date);
}

/** @param {string} input */
function sanitizeHtml(input) {
	const container = /** @type {HTMLDivElement & { setHTML?: (input: string) => void }} */ (
		document.createElement('div')
	);

	if (typeof container.setHTML === 'function') {
		container.setHTML(input || '');
	} else {
		container.textContent = input || '';
	}

	container.querySelectorAll('a').forEach(node => {
		node.target = '_blank';
		node.rel = 'noopener noreferrer';
	});

	container.querySelectorAll('video').forEach(node => {
		node.controls = true;
		node.autoplay = false;
	});

	container.querySelectorAll('img').forEach(node => {
		node.loading = 'lazy';
		node.decoding = 'async';
	});

	const fragment = document.createDocumentFragment();
	fragment.append(...container.childNodes);
	return fragment;
}

/** @param {string} value */
function safeArticleUrl(value) {
	try {
		const href = new URL(value, FEED_URL).href;
		return href.startsWith('https://github.blog/') ? href : '';
	} catch {
		return '';
	}
}

/** @param {string[]} values */
function unique(values) {
	return [...new Set(values.filter(Boolean))].sort((a, b) => a.localeCompare(b));
}

/**
 * @param {HTMLSelectElement} select
 * @param {string[]} values
 * @param {string} label
 */
function fillSelect(select, values, label) {
	const current = select.value;
	select.replaceChildren(new Option(label, ''));
	values.forEach(value => select.add(new Option(value, value)));
	if (values.includes(current)) select.value = current;
}

/** @param {string} message */
function showError(message) {
	el.errorBox.hidden = false;
	el.errorBox.textContent = message;
}

function clearError() {
	el.errorBox.hidden = true;
	el.errorBox.textContent = '';
}

/** @param {FeedItem} item */
function itemSearchText(item) {
	const content = sanitizeHtml(item.content);
	return [
		item.title,
		item.author,
		item.pubDate,
		item.types.join(' '),
		item.labels.join(' '),
		content.textContent || '',
	].join(' ').toLowerCase();
}

/** @param {{ preserveArticle?: boolean }} [options] */
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
	const itemNodes = /** @type {NodeListOf<HTMLElement>} */ (
		el.items.querySelectorAll('.item')
	);
	const anchor = [...itemNodes]
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
		? [
			.../** @type {NodeListOf<HTMLElement>} */ (
				el.items.querySelectorAll('.item')
			),
		].find(node => node.dataset.id === anchorId)
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
		el.article.textContent = 'No entry selected.';
		return;
	}

	const header = document.createElement('header');
	header.className = 'article-head';

	const title = document.createElement('h2');
	title.textContent = item.title;

	const meta = document.createElement('div');
	meta.className = 'meta';

	const actions = document.createElement('div');
	actions.className = 'article-actions';

	header.append(title, meta, actions);

	const content = document.createElement('article');
	content.className = 'article-content';

	el.article.className = '';
	el.article.replaceChildren(header, content);

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

	const href = safeArticleUrl(item.link);
	if (href) {
		const link = document.createElement('a');
		link.href = href;
		link.target = '_blank';
		link.rel = 'noopener noreferrer';
		link.textContent = 'Open original ↗';
		actions.append(link);
	}

	const safeContent = sanitizeHtml(item.content);
	if (safeContent.hasChildNodes()) {
		content.append(safeContent);
	} else {
		const empty = document.createElement('p');
		empty.textContent = 'No content in this entry.';
		content.append(empty);
	}
}

const MAX_PAGES = 50;
const DB_NAME = 'github-actions-changelog-reader';
const DB_VERSION = 1;
const STORE_NAME = 'feeds';

/** @param {FeedItem[]} items */
function sortItems(items) {
	items.sort((a, b) => {
		const aTime = parseDate(a.pubDate)?.getTime() ?? 0;
		const bTime = parseDate(b.pubDate)?.getTime() ?? 0;
		return bTime - aTime;
	});
}

/** @param {{ preserveArticle?: boolean }} [options] */
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

	el.raw.textContent = state.rawXml;
	applyFilters({ preserveArticle });
}

/**
 * @param {Feed} feed
 * @param {string} rawXml
 * @param {{ complete?: boolean, preserveArticle?: boolean, nextPage?: number }} [options]
 */
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

/**
 * @param {Feed} feedPage
 * @param {{ preserveArticle?: boolean }} [options]
 * @returns {{ added: number, updated: number }}
 */
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

/** @returns {Promise<IDBDatabase>} */
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

/** @returns {Promise<CachedFeed | null>} */
async function readCache() {
	try {
		const db = await openDb();
		return await new Promise((resolve, reject) => {
			const tx = db.transaction(STORE_NAME, 'readonly');
			const request = tx.objectStore(STORE_NAME).get(FEED_URL);
			request.onerror = () => reject(request.error);
			request.onsuccess = () =>
				resolve(
					/** @type {CachedFeed | null} */ (request.result ?? null),
				);
			tx.oncomplete = () => db.close();
		});
	} catch {
		return null;
	}
}

/** @param {boolean} [complete] */
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
			tx.oncomplete = () => resolve(undefined);
		});
		db.close();
	} catch {
		// Caching is an optimization. Reading the feed must keep working without it.
	}
}

/**
 * @param {number} page
 * @returns {Promise<{ url: string, xmlText: string, feed: Feed } | null>}
 */
function fetchFeedPage(page) {
	const url = new URL(FEED_URL);
	if (page > 1) url.searchParams.set('paged', String(page));
	url.searchParams.set('_', String(Date.now()));

	return new Promise((resolve, reject) => {
		const request = new XMLHttpRequest();
		request.open('GET', url.href);
		request.overrideMimeType('application/rss+xml');
		request.setRequestHeader(
			'Accept',
			'application/rss+xml, application/xml, text/xml, */*',
		);
		request.timeout = 30_000;

		request.onload = () => {
			if (page > 1 && request.status === 404) {
				resolve(null);
				return;
			}
			if (request.status < 200 || request.status >= 300) {
				reject(new Error(`HTTP ${request.status} ${request.statusText}`));
				return;
			}

			try {
				resolve({
					url: url.href,
					xmlText: request.responseText,
					feed: parseFeed(request.responseText),
				});
			} catch (error) {
				reject(error);
			}
		};
		request.onerror = () => reject(new Error('Network request failed.'));
		request.ontimeout = () => reject(new Error('The feed request timed out.'));
		request.send();
	});
}

/** @returns {Promise<void>} */
function nextFrame() {
	return new Promise(resolve => requestAnimationFrame(() => resolve(undefined)));
}

function hasActiveHistoryQuery() {
	return Boolean(el.search.value.trim() || el.typeFilter.value);
}

/** @param {number} [page] */
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
		el.raw.textContent = state.rawXml;

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
			el.raw.textContent = state.rawXml;

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
			el.raw.textContent = state.rawXml;
		}

		state.pageSize = Math.max(1, first.feed.items.length);
		state.nextPage = Math.max(2, state.nextPage);

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
				el.raw.textContent = state.rawXml;

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

/** @type {number | null} */
let searchTimer = null;
el.search.addEventListener('input', () => {
	applyFilters();
	if (searchTimer !== null) window.clearTimeout(searchTimer);

	if (el.search.value.trim()) {
		searchTimer = window.setTimeout(() => void loadAllHistory(), 120);
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
