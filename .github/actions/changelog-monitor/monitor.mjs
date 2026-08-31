// @ts-check

/** @typedef {import('@actions/github-script').AsyncFunctionArguments} AsyncFunctionArguments */

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

export const FEED_URL = 'https://github.blog/changelog/label/actions/feed/';
export const FEED_LABEL = 'actions';
export const LABEL = 'github-changelog';
export const LABEL_COLOR = '2088FF';
export const LABEL_DESCRIPTION = 'GitHub Actions entries from the GitHub Changelog feed';

const MARKER_PREFIX = '<!-- github-changelog-guid: ';
const MARKER_SUFFIX = ' -->';
const MARKER_PATTERN = /<!-- github-changelog-guid: (\S+) -->/g;

/** @param {string} guid */
export function marker(guid) {
	return `${MARKER_PREFIX}${guid}${MARKER_SUFFIX}`;
}

const ENTITIES = new Map([
	['amp', '&'],
	['lt', '<'],
	['gt', '>'],
	['quot', '"'],
	['apos', "'"],
	['nbsp', ' '],
]);

/** @param {string} value */
function decodeEntities(value) {
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

/** @param {string} value */
function unwrap(value) {
	const cdata = /^\s*<!\[CDATA\[([\s\S]*)\]\]>\s*$/.exec(value);
	return decodeEntities(cdata === null ? value : cdata[1]).trim();
}

/**
 * @param {string} item
 * @param {string} tag
 */
function element(item, tag) {
	const match = new RegExp(`<${tag}(?:\\s[^>]*)?>([\\s\\S]*?)</${tag}>`).exec(item);
	return match === null ? undefined : unwrap(match[1]);
}

/** @param {string} html */
function plainText(html) {
	return decodeEntities(html.replace(/<[^>]*>/g, ' ')).replace(/\s+/g, ' ').trim();
}

/**
 * Reduce an item description to its lead paragraph, dropping the syndication footer.
 *
 * @param {string} description
 */
function summarize(description) {
	const paragraphs = [...description.matchAll(/<p>([\s\S]*?)<\/p>/g)].map((match) => plainText(match[1]));
	const lead = paragraphs.find((paragraph) => paragraph !== '' && !paragraph.startsWith('The post '));
	return lead ?? plainText(description);
}

/**
 * Read the CDATA values of one category domain.
 *
 * @param {string} item
 * @param {string} domain
 * @returns {string[]}
 */
function categories(item, domain) {
	const pattern = new RegExp(`<category domain="${domain}">([\\s\\S]*?)</category>`, 'g');
	return [...item.matchAll(pattern)].map((match) => unwrap(match[1]));
}

/**
 * @param {string} xml
 * @returns {ChangelogEntry[]}
 */
export function parseFeed(xml) {
	/** @type {ChangelogEntry[]} */
	const entries = [];
	for (const match of xml.matchAll(/<item>([\s\S]*?)<\/item>/g)) {
		const item = match[1];
		const link = element(item, 'link');
		const title = element(item, 'title');
		const pubDate = element(item, 'pubDate');
		if (link === undefined || title === undefined || pubDate === undefined) {
			throw new Error(`feed item is missing link, title, or pubDate: ${item.slice(0, 120)}`);
		}
		const published = new Date(pubDate);
		if (Number.isNaN(published.getTime())) {
			throw new Error(`feed item ${link} has an unparsable pubDate ${JSON.stringify(pubDate)}`);
		}
		entries.push({
			guid: element(item, 'guid') ?? link,
			link,
			title,
			published: published.toISOString(),
			summary: summarize(element(item, 'description') ?? ''),
			type: categories(item, 'changelog-type')[0] ?? '',
			labels: categories(item, 'changelog-label'),
		});
	}
	return entries;
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

/** @param {string} url */
async function fetchFeed(url) {
	const response = await fetch(url, { headers: { accept: 'application/rss+xml, application/xml' } });
	if (!response.ok) {
		throw new Error(`${url} responded ${response.status} ${response.statusText}`);
	}
	return response.text();
}

/**
 * Workflow entry point.
 *
 * @param {AsyncFunctionArguments} args
 */
export default async function run({ github, context, core }) {
	const { owner, repo } = context.repo;
	const lookbackDays = positiveInteger(process.env, 'LOOKBACK_DAYS', 30);
	const limit = positiveInteger(process.env, 'MAX_ISSUES', 10);
	const since = new Date(Date.now() - lookbackDays * 24 * 60 * 60 * 1000);

	const entries = parseFeed(await fetchFeed(process.env.FEED_URL ?? FEED_URL));
	if (entries.length === 0) {
		throw new Error('feed carries no entries');
	}
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
