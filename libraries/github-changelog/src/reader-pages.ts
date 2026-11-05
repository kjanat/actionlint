import type { Feed } from '#feed';

// Historical RSS pages have their own dates. Only page 1 describes the current feed.
export function updateFeedMetadata(feed: Feed, page: Feed, pageNumber: number): void {
	if (pageNumber !== 1) return;
	feed.title = page.title;
	feed.description = page.description;
	feed.lastBuildDate = page.lastBuildDate;
}

export function readRawPages(rawXml: string): Map<number, string> {
	const pages = new Map<number, string>();
	const marker = /^<!-- Feed page (\d+) -->\r?\n/gm;
	const matches = [...rawXml.matchAll(marker)];
	if (!matches.length) {
		if (rawXml) pages.set(1, rawXml);
		return pages;
	}
	for (const [index, match] of matches.entries()) {
		pages.set(Number(match[1]), rawXml.slice(match.index + match[0].length, matches[index + 1]?.index).trimEnd());
	}
	return pages;
}
