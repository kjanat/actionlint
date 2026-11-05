export interface Category {
	domain: string;
	value: string;
}

export interface FeedItem {
	id: string;
	title: string;
	link: string;
	guid: string;
	author: string;
	pubDate: string;
	description: string;
	content: string;
	categories: Category[];
	types: string[];
	labels: string[];
}

export interface Feed {
	title: string;
	link: string;
	description: string;
	language: string;
	lastBuildDate: string;
	generator: string;
	image: string;
	items: FeedItem[];
}

export const FEED_URL = 'https://github.blog/changelog/label/actions/feed/';

export function feedPageUrl(page: number, cacheKey: number = Date.now()): string {
	const url = new URL(FEED_URL);
	if (page > 1) url.searchParams.set('paged', String(page));
	else url.searchParams.set('_', String(cacheKey));
	return url.href;
}

const ENTITIES = new Map([
	['amp', '&'],
	['lt', '<'],
	['gt', '>'],
	['quot', '"'],
	['apos', "'"],
	['nbsp', ' '],
]);

export function decodeEntities(value: string): string {
	return value.replace(/&(#x?[0-9a-fA-F]+|[a-zA-Z]+);/g, (whole, name: string) => {
		if (name.startsWith('#x') || name.startsWith('#X')) {
			return String.fromCodePoint(Number.parseInt(name.slice(2), 16));
		}
		if (name.startsWith('#')) {
			return String.fromCodePoint(Number.parseInt(name.slice(1), 10));
		}
		return ENTITIES.get(name) ?? whole;
	});
}

function unwrap(value: string): string {
	const cdata = /^\s*<!\[CDATA\[([\s\S]*)\]\]>\s*$/.exec(value)?.[1];
	return (cdata === undefined ? decodeEntities(value) : cdata).trim();
}

export interface Block {
	attributes: string;
	inner: string;
}

export function* blocks(source: string, tag: string): Generator<Block> {
	const open = `<${tag}`;
	const close = `</${tag}>`;
	let from = 0;

	for (;;) {
		const start = source.indexOf(open, from);
		if (start === -1) return;

		const afterName = start + open.length;
		const delimiter = source[afterName];
		if (delimiter !== '>' && delimiter !== ' ' && delimiter !== '\t' && delimiter !== '\n' && delimiter !== '\r') {
			from = afterName;
			continue;
		}

		const openEnd = source.indexOf('>', afterName);
		if (openEnd === -1) return;
		const end = source.indexOf(close, openEnd + 1);
		if (end === -1) return;

		yield { attributes: source.slice(afterName, openEnd), inner: source.slice(openEnd + 1, end) };
		from = end + close.length;
	}
}

function firstBlock(source: string, tag: string): Block | undefined {
	for (const block of blocks(source, tag)) return block;
	return undefined;
}

function element(scope: string, tag: string): string {
	for (const block of blocks(scope, tag)) return unwrap(block.inner);
	return '';
}

function categories(item: string): Category[] {
	return [...blocks(item, 'category')]
		.map((block) => ({
			domain: /domain="([^"]*)"/.exec(block.attributes)?.[1] ?? '',
			value: unwrap(block.inner),
		}))
		.filter((category) => category.value !== '');
}

function valuesOf(cats: Category[], domain: string): string[] {
	return cats.filter((category) => category.domain === domain).map((category) => category.value);
}

export function parseFeed(xmlText: string): Feed {
	const source = String(xmlText ?? '');
	const channel = firstBlock(source, 'channel');
	if (channel === undefined) {
		throw new Error('No RSS <channel> found. This viewer currently targets RSS 2.0 feeds.');
	}

	const image = firstBlock(channel.inner, 'image');
	const head = image === undefined
		? channel.inner
		: channel.inner.replace(`<image${image.attributes}>${image.inner}</image>`, '');
	const imageBlock = image?.inner ?? '';

	const items: FeedItem[] = [];
	for (const { inner: item } of blocks(channel.inner, 'item')) {
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
