// @ts-check

import { blocks, decodeEntities, FEED_URL, parseFeed } from './github-actions-feed.js';

/** @typedef {import('@actions/github-script').AsyncFunctionArguments} AsyncFunctionArguments */
/** @typedef {Pick<AsyncFunctionArguments, 'github' | 'context' | 'core'>} RunArguments */
/** @typedef {import('#feed').FeedItem} FeedItem */

/**
 * @typedef ChangelogEntry
 * @property {string} guid
 * @property {string} link
 * @property {string} title
 * @property {string} published
 * @property {string} summary
 * @property {string} type
 * @property {string[]} labels
 */

export const LABEL = 'github-changelog';
export const LABEL_COLOR = '2088FF';
export const LABEL_DESCRIPTION = 'GitHub Actions entries from the GitHub Changelog feed';
export const FEED_LABEL = 'actions';

const MARKER_PREFIX = '<!-- github-changelog-guid: ';
const MARKER_SUFFIX = ' -->';
const MARKER_PATTERN = /<!-- github-changelog-guid: (\S+) -->/g;

/** @param {string} guid */
export function marker(guid) {
	return `${MARKER_PREFIX}${guid}${MARKER_SUFFIX}`;
}

/** @param {string} html */
function stripTags(html) {
	let text = '';
	let from = 0;

	for (;;) {
		const start = html.indexOf('<', from);
		if (start === -1) return text + html.slice(from);
		const end = html.indexOf('>', start + 1);
		if (end === -1) return text + html.slice(from);
		text += `${html.slice(from, start)} `;
		from = end + 1;
	}
}

/** @param {string} html */
function plainText(html) {
	return decodeEntities(stripTags(html)).replace(/\s+/g, ' ').trim();
}

/**
 * Reduce an item description to its lead paragraph, dropping the syndication footer.
 *
 * @param {string} description
 */
function summarize(description) {
	const paragraphs = [...blocks(description, 'p')].map((block) => plainText(block.inner));
	const lead = paragraphs.find((paragraph) => paragraph !== '' && !paragraph.startsWith('The post '));
	return lead ?? plainText(description);
}

/**
 * @param {FeedItem} item
 * @returns {ChangelogEntry}
 */
export function toEntry(item) {
	const published = new Date(item.pubDate);
	if (Number.isNaN(published.getTime())) {
		throw new Error(`feed item ${item.link} has an unparsable pubDate ${JSON.stringify(item.pubDate)}`);
	}
	return {
		guid: item.guid || item.link,
		link: item.link,
		title: item.title,
		published: published.toISOString(),
		summary: summarize(item.description),
		type: item.types[0] ?? '',
		labels: item.labels,
	};
}

/**
 * @param {Iterable<{ body?: string | null }>} issues
 * @returns {Set<string>}
 */
export function reportedGuids(issues) {
	/** @type {Set<string>} */
	const guids = new Set();
	for (const issue of issues) {
		if (typeof issue.body !== 'string') {
			continue;
		}
		for (const match of issue.body.matchAll(MARKER_PATTERN)) {
			guids.add(match[1]);
		}
	}
	return guids;
}

/**
 * Oldest first, so issue numbers follow publication order.
 *
 * @param {ChangelogEntry[]} entries
 * @param {{ reported: Set<string>, since: Date, limit: number }} options
 */
export function selectEntries(entries, { reported, since, limit }) {
	return entries
		.filter((entry) =>
			entry.labels.includes(FEED_LABEL)
			&& !reported.has(entry.guid)
			&& new Date(entry.published) >= since
		)
		.sort((left, right) => left.published.localeCompare(right.published))
		.slice(0, limit);
}

/** @param {ChangelogEntry} entry */
export function issueTitle(entry) {
	return `Changelog: ${entry.title}`;
}

/** @param {ChangelogEntry} entry */
export function issueBody(entry) {
	const summary = entry.summary === '' ? '' : `\n\n${entry.summary}`;
	const type = entry.type === '' ? '' : ` ${entry.type}.`;
	const topics = entry.labels.length === 0 ? '' : `\n\nChangelog labels: ${entry.labels.join(', ')}.`;
	return `${marker(entry.guid)}

${entry.link}

Published ${entry.published.slice(0, 10)}.${type}${summary}${topics}

Opened by the GitHub Changelog monitor workflow. Close it once the change is evaluated against this fork.`;
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @param {string} name
 * @param {number} fallback
 */
function positiveInteger(env, name, fallback) {
	const value = env[name] ?? '';
	if (value === '') {
		return fallback;
	}
	if (!/^[1-9]\d*$/.test(value)) {
		throw new Error(`${name} must be a positive integer, got ${JSON.stringify(value)}`);
	}
	return Number(value);
}

/**
 * @param {{
 *   github: AsyncFunctionArguments['github'],
 *   context: AsyncFunctionArguments['context'],
 *   core: AsyncFunctionArguments['core'],
 * }} options
 */
async function ensureLabel({ github, context, core }) {
	const { owner, repo } = context.repo;
	try {
		await github.rest.issues.getLabel({ owner, repo, name: LABEL });
		return;
	} catch (error) {
		if (!(error instanceof Object) || !('status' in error) || error.status !== 404) {
			throw error;
		}
	}
	await github.rest.issues.createLabel({
		owner,
		repo,
		name: LABEL,
		color: LABEL_COLOR,
		description: LABEL_DESCRIPTION,
	});
	core.info(`Created label ${LABEL}`);
}

/** The feed is a fixed endpoint, so no caller can steer the request. */
async function fetchFeed() {
	const response = await fetch(FEED_URL, { headers: { accept: 'application/rss+xml, application/xml' } });
	if (!response.ok) {
		throw new Error(`${FEED_URL} responded ${response.status} ${response.statusText}`);
	}
	return response.text();
}

/**
 * Workflow entry point.
 *
 * @param {RunArguments} args
 */
export default async function run({ github, context, core }) {
	const { owner, repo } = context.repo;
	const lookbackDays = positiveInteger(process.env, 'LOOKBACK_DAYS', 30);
	const limit = positiveInteger(process.env, 'MAX_ISSUES', 10);
	const since = new Date(Date.now() - lookbackDays * 24 * 60 * 60 * 1000);

	const feed = parseFeed(await fetchFeed());
	if (feed.items.length === 0) {
		throw new Error('feed carries no entries');
	}
	const entries = feed.items.map(toEntry);
	core.info(`Feed carries ${entries.length} entries`);

	await ensureLabel({ github, context, core });
	const issues = await github.paginate(github.rest.issues.listForRepo, {
		owner,
		repo,
		state: 'all',
		labels: LABEL,
		per_page: 100,
	});
	const selected = selectEntries(entries, { reported: reportedGuids(issues), since, limit });
	if (selected.length === 0) {
		core.info('No unreported entries within the lookback window');
		core.setOutput('issues-created', 0);
		core.setOutput('issue-urls', '');
		return;
	}

	/** @type {string[]} */
	const opened = [];
	for (const entry of selected) {
		const created = await github.rest.issues.create({
			owner,
			repo,
			title: issueTitle(entry),
			body: issueBody(entry),
			labels: [LABEL],
		});
		opened.push(created.data.html_url);
		core.info(`Opened ${created.data.html_url} for ${entry.link}`);
	}
	core.setOutput('issues-created', opened.length);
	core.setOutput('issue-urls', opened.join('\n'));
	await core.summary
		.addHeading('GitHub Changelog monitor', 3)
		.addList(selected.map((entry) => `${entry.published.slice(0, 10)} \u2014 ${entry.title}`))
		.write();
}
