import assert from 'node:assert/strict';
import test from 'node:test';

import { parseFeed } from '../../../playground/public/github-changelog/feed.mjs';
import { issueBody, issueTitle, LABEL, marker, reportedGuids, selectEntries, toEntry } from './monitor.mjs';

const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<title>Use Case: actions - GitHub Changelog</title>
<item>
	<title>Actions steps can now be run in parallel</title>
	<link>https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel</link>
	<pubDate>Thu, 25 Jun 2026 16:46:09 +0000</pubDate>
	<guid isPermaLink="false">https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel</guid>
	<description><![CDATA[<p>Steps in a job can now run at the same time&#8230;</p>
<p>The post <a href="https://github.blog">Actions steps</a> appeared first on <a href="https://github.blog">The GitHub Blog</a>.</p>]]></description>
	<category domain="changelog-type"><![CDATA[Release]]></category>
	<category domain="changelog-label"><![CDATA[actions]]></category>
</item>
<item>
	<title>GitHub Actions holds potentially malicious workflows for approval</title>
	<link>https://github.blog/changelog/2026-07-28-github-actions-holds-potentially-malicious-workflows-for-approval</link>
	<pubDate>Tue, 28 Jul 2026 11:57:19 +0000</pubDate>
	<guid isPermaLink="false">https://github.blog/changelog/2026-07-28-github-actions-holds-unproven-workflows-for-approval</guid>
	<description><![CDATA[<p>Workflows wait for approval.</p>]]></description>
	<category domain="changelog-type"><![CDATA[Improvement]]></category>
	<category domain="changelog-label"><![CDATA[actions]]></category>
	<category domain="changelog-label"><![CDATA[supply chain security]]></category>
</item>
</channel></rss>`;

const entries = () => parseFeed(feed).items.map(toEntry);

test('toEntry carries the categories and the lead paragraph', () => {
	assert.deepEqual(entries()[0], {
		guid: 'https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel',
		link: 'https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel',
		title: 'Actions steps can now be run in parallel',
		published: '2026-06-25T16:46:09.000Z',
		summary: 'Steps in a job can now run at the same time…',
		type: 'Release',
		labels: ['actions'],
	});
});

test('toEntry keeps the guid when it diverges from the link', () => {
	const entry = entries()[1];
	assert.equal(
		entry.guid,
		'https://github.blog/changelog/2026-07-28-github-actions-holds-unproven-workflows-for-approval',
	);
	assert.notEqual(entry.guid, entry.link);
});

test('toEntry rejects an unparsable date', () => {
	assert.throws(
		() => toEntry({ ...parseFeed(feed).items[0], pubDate: 'whenever' }),
		/unparsable pubDate/,
	);
});

test('reportedGuids collects markers and ignores bodyless issues', () => {
	const guids = reportedGuids([
		{ body: `${marker('a')}\n\nhttps://example.com` },
		{ body: null },
		{ body: 'no marker here' },
		{ body: marker('b') },
	]);
	assert.deepEqual([...guids], ['a', 'b']);
});

test('selectEntries drops reported entries and anything before the window', () => {
	const selected = selectEntries(entries(), {
		reported: new Set(['https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel']),
		since: new Date('2026-01-01T00:00:00Z'),
		limit: 10,
	});
	assert.deepEqual(selected.map((entry) => entry.title), [
		'GitHub Actions holds potentially malicious workflows for approval',
	]);

	assert.deepEqual(
		selectEntries(entries(), { reported: new Set(), since: new Date('2026-08-01T00:00:00Z'), limit: 10 }),
		[],
	);
});

test('selectEntries drops an entry the feed does not label actions', () => {
	const unrelated = feed.replace(
		'<category domain="changelog-label"><![CDATA[actions]]></category>\n\t<category domain="changelog-label"><![CDATA[supply chain security]]></category>',
		'<category domain="changelog-label"><![CDATA[copilot]]></category>',
	);
	const selected = selectEntries(parseFeed(unrelated).items.map(toEntry), {
		reported: new Set(),
		since: new Date('2026-01-01T00:00:00Z'),
		limit: 10,
	});
	assert.deepEqual(selected.map((entry) => entry.title), ['Actions steps can now be run in parallel']);
});

test('issue title and body carry the dedupe marker and the link', () => {
	const entry = entries()[1];
	assert.equal(issueTitle(entry), 'Changelog: GitHub Actions holds potentially malicious workflows for approval');
	const body = issueBody(entry);
	assert.equal(reportedGuids([{ body }]).has(entry.guid), true);
	assert.match(body, /Published 2026-07-28\. Improvement\./);
	assert.match(body, /Workflows wait for approval\./);
	assert.match(body, /Changelog labels: actions, supply chain security\./);
	assert.equal(LABEL, 'github-changelog');
});
