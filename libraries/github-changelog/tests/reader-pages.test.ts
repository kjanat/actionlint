import assert from 'node:assert/strict';
import test from 'node:test';

import { readRawPages, updateFeedMetadata } from '#/reader-pages';
import { parseFeed } from '#feed';

function feed(date: string) {
	return parseFeed(`<rss><channel><title>Actions</title><lastBuildDate>${date}</lastBuildDate></channel></rss>`);
}

await test('history pagination never replaces current channel metadata', () => {
	const current = feed('Thu, 10 Sep 2026 18:11:22 +0000');
	const history = feed('Sat, 22 Mar 2025 13:47:25 +0000');
	history.title = 'Historical page';
	updateFeedMetadata(current, history, 38);
	assert.equal(current.lastBuildDate, 'Thu, 10 Sep 2026 18:11:22 +0000');
	assert.equal(current.title, 'Actions');
});

await test('refresh repairs a historical cached date and clears missing feed dates', () => {
	const cached = feed('Sat, 22 Mar 2025 13:47:25 +0000');
	updateFeedMetadata(cached, feed('Thu, 10 Sep 2026 18:11:22 +0000'), 1);
	assert.equal(cached.lastBuildDate, 'Thu, 10 Sep 2026 18:11:22 +0000');
	updateFeedMetadata(cached, feed(''), 1);
	assert.equal(cached.lastBuildDate, '');
});

await test('raw pages support original cache, numbered history, CRLF and empty state', () => {
	assert.deepEqual([...readRawPages('')], []);
	assert.deepEqual([...readRawPages('<rss/>')], [[1, '<rss/>']]);
	assert.deepEqual([
		...readRawPages('<!-- Feed page 1 -->\r\n<rss>current</rss>\r\n\r\n<!-- Feed page 38 -->\r\n<rss>old</rss>'),
	], [
		[1, '<rss>current</rss>'],
		[38, '<rss>old</rss>'],
	]);
});
