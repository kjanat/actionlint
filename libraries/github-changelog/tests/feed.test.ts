import assert from 'node:assert/strict';
import test from 'node:test';

import { decodeEntities, feedPageUrl, parseFeed } from '#feed';

const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<title>Use Case: actions - GitHub Changelog</title>
<link>https://github.blog/changelog/label/actions/</link>
<description>Updates from GitHub.</description>
<language>en-US</language>
<lastBuildDate>Fri, 28 Aug 2026 21:31:02 +0000</lastBuildDate>
<generator>https://wordpress.org/?v=7.0.4</generator>
<image>
	<url>https://github.blog/favicon.png</url>
	<title>Use Case: actions - GitHub Changelog</title>
	<link>https://github.blog/changelog/</link>
</image>
<item>
	<title>Read-only Actions cache for untrusted triggers</title>
	<link>https://github.blog/changelog/2026-06-26-read-only-actions-cache-for-untrusted-triggers</link>
	<dc:creator><![CDATA[Allison]]></dc:creator>
	<pubDate>Fri, 26 Jun 2026 18:31:43 +0000</pubDate>
	<guid isPermaLink="false">https://github.blog/changelog/2026-06-26-read-only-cache</guid>
	<description><![CDATA[<p>Caches are read-only&#8230;</p>]]></description>
	<content:encoded><![CDATA[<p>It won&rsquo;t write. &lt;script&gt;alert(1)&lt;/script&gt;</p>]]></content:encoded>
	<category domain="changelog-type"><![CDATA[Improvement]]></category>
	<category domain="changelog-label"><![CDATA[actions]]></category>
	<category domain="changelog-label"><![CDATA[supply chain security]]></category>
</item>
</channel></rss>`;

await test('channel metadata does not come from the image or the items', () => {
	const parsed = parseFeed(feed);
	assert.equal(parsed.title, 'Use Case: actions - GitHub Changelog');
	assert.equal(parsed.link, 'https://github.blog/changelog/label/actions/');
	assert.equal(parsed.description, 'Updates from GitHub.');
	assert.equal(parsed.language, 'en-US');
	assert.equal(parsed.lastBuildDate, 'Fri, 28 Aug 2026 21:31:02 +0000');
	assert.equal(parsed.generator, 'https://wordpress.org/?v=7.0.4');
	assert.equal(parsed.image, 'https://github.blog/favicon.png');
	assert.equal(parsed.items.length, 1);
});

await test('an item exposes its fields, namespaced tags and categories', () => {
	const [item] = parseFeed(feed).items;
	assert.ok(item);
	assert.equal(item.id, 'https://github.blog/changelog/2026-06-26-read-only-cache');
	assert.equal(item.guid, 'https://github.blog/changelog/2026-06-26-read-only-cache');
	assert.equal(item.link, 'https://github.blog/changelog/2026-06-26-read-only-actions-cache-for-untrusted-triggers');
	assert.equal(item.author, 'Allison');
	assert.equal(item.pubDate, 'Fri, 26 Jun 2026 18:31:43 +0000');
	assert.deepEqual(item.types, ['Improvement']);
	assert.deepEqual(item.labels, ['actions', 'supply chain security']);
	assert.deepEqual(item.categories.map((category) => category.domain), [
		'changelog-type',
		'changelog-label',
		'changelog-label',
	]);
});

await test('CDATA markup reaches the caller as authored', () => {
	const [item] = parseFeed(feed).items;
	assert.ok(item);
	assert.equal(item.content, '<p>It won&rsquo;t write. &lt;script&gt;alert(1)&lt;/script&gt;</p>');
	assert.equal(item.content.includes('<script>'), false);
	assert.equal(item.description, '<p>Caches are read-only&#8230;</p>');
});

await test('content falls back to the description', () => {
	const [item] = parseFeed(feed.replace(/<content:encoded>[\s\S]*?<\/content:encoded>/, '')).items;
	assert.ok(item);
	assert.equal(item.content, item.description);
});

await test('a document without a channel is rejected', () => {
	assert.throws(() => parseFeed('<html><body>not a feed</body></html>'), /No RSS <channel> found/);
});

await test('decodeEntities resolves named, decimal and hexadecimal references', () => {
	assert.equal(decodeEntities('a &amp; b &#8230; c &#x2014; d &unknown;'), 'a & b … c — d &unknown;');
});

await test('only the latest feed page gets a cache-busting parameter', () => {
	assert.equal(feedPageUrl(1, 123), 'https://github.blog/changelog/label/actions/feed/?_=123');
	assert.equal(feedPageUrl(2, 123), 'https://github.blog/changelog/label/actions/feed/?paged=2');
});
