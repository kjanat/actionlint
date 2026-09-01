/**
 * @typedef Category
 * @property {string} domain
 * @property {string} value
 */

/**
 * @typedef FeedItem
 * @property {string} id
 * @property {string} title
 * @property {string} link
 * @property {string} guid
 * @property {string} author
 * @property {string} pubDate
 * @property {string} description
 * @property {string} content
 * @property {Category[]} categories
 * @property {string[]} types
 * @property {string[]} labels
 */

/**
 * @typedef Feed
 * @property {string} title
 * @property {string} link
 * @property {string} description
 * @property {string} language
 * @property {string} lastBuildDate
 * @property {string} generator
 * @property {string} image
 * @property {FeedItem[]} items
 */

export const FEED_URL = 'https://github.blog/changelog/label/actions/feed/';

const ENTITIES = new Map([
	['amp', '&'],
	['lt', '<'],
	['gt', '>'],
	['quot', '"'],
	['apos', "'"],
	['nbsp', ' '],
]);

/**
 * Resolve XML and HTML character references.
 *
 * @param {string} value
 */
export function decodeEntities(value) {
	return value.replace(/&(#x?[0-9a-fA-F]+|[a-zA-Z]+);/g, (whole, name) => {
		if (name.startsWith('#x') || name.startsWith('#X')) {
			return String.fromCodePoint(Number.parseInt(name.slice(2), 16));
		}
		if (name.startsWith('#')) {
			return String.fromCodePoint(Number.parseInt(name.slice(1), 10));
		}
		return ENTITIES.get(name) ?? whole;
	});
}

/**
 * CDATA is markup-free by definition, so its text is returned as authored.
 *
 * @param {string} value
 */
function unwrap(value) {
	const cdata = /^\s*<!\[CDATA\[([\s\S]*)\]\]>\s*$/.exec(value)?.[1];
	return (cdata === undefined ? decodeEntities(value) : cdata).trim();
}

/**
 * @param {string} scope
 * @param {string} tag
 */
function element(scope, tag) {
	const inner = new RegExp(`<${tag}(?:\\s[^>]*)?>([\\s\\S]*?)</${tag}>`).exec(scope)?.[1];
	return inner === undefined ? '' : unwrap(inner);
}

/**
 * @param {string} item
 * @returns {Category[]}
 */
function categories(item) {
	return [...item.matchAll(/<category(\s[^>]*)?>([\s\S]*?)<\/category>/g)]
		.map(([, attributes = '', value = '']) => ({
			domain: /domain="([^"]*)"/.exec(attributes)?.[1] ?? '',
			value: unwrap(value),
		}))
		.filter((category) => category.value !== '');
}

/**
 * @param {Category[]} cats
 * @param {string} domain
 */
function valuesOf(cats, domain) {
	return cats.filter((category) => category.domain === domain).map((category) => category.value);
}

/**
 * Parse an RSS 2.0 document without a DOM, so the reader and the changelog
 * monitor share one implementation.
 *
 * @param {string} xmlText
 * @returns {Feed}
 */
export function parseFeed(xmlText) {
	const source = String(xmlText ?? '');
	if (!/<channel(?:\s[^>]*)?>/.test(source)) {
		throw new Error('No RSS <channel> found. This viewer currently targets RSS 2.0 feeds.');
	}

	const firstItem = source.search(/<item(?:\s[^>]*)?>/);
	const head = (firstItem === -1 ? source : source.slice(0, firstItem))
		.replace(/<image(?:\s[^>]*)?>[\s\S]*?<\/image>/, '');
	const imageBlock = /<image(?:\s[^>]*)?>([\s\S]*?)<\/image>/.exec(source)?.[1] ?? '';

	/** @type {FeedItem[]} */
	const items = [];
	for (const [, item = ''] of source.matchAll(/<item(?:\s[^>]*)?>([\s\S]*?)<\/item>/g)) {
		const cats = categories(item);
		const description = element(item, 'description');
		const encoded = element(item, 'content:encoded');
		const guid = element(item, 'guid');
		const link = element(item, 'link');

		items.push({
			id: guid || link || String(items.length),
			title: element(item, 'title') || '(untitled)',
			link,
			guid,
			author: element(item, 'dc:creator') || element(item, 'author'),
			pubDate: element(item, 'pubDate'),
			description,
			content: encoded || description,
			categories: cats,
			types: valuesOf(cats, 'changelog-type'),
			labels: valuesOf(cats, 'changelog-label'),
		});
	}

	return {
		title: element(head, 'title') || 'RSS feed',
		link: element(head, 'link'),
		description: element(head, 'description'),
		language: element(head, 'language'),
		lastBuildDate: element(head, 'lastBuildDate'),
		generator: element(head, 'generator'),
		image: element(imageBlock, 'url'),
		items,
	};
}
