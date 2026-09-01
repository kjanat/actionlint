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

export const FEED_URL: string;

export interface Block {
	attributes: string;
	inner: string;
}

export function blocks(source: string, tag: string): Generator<Block>;

export function decodeEntities(value: string): string;

export function parseFeed(xmlText: string): Feed;
