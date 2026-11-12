const anchors = /^<a id="(v\d+\.\d+\.\d+)"><\/a>\r?$/gm;
const heading =
	/^#{1,2} \[(v\d+\.\d+\.\d+)\]\((https:\/\/\S+\/releases\/tag\/(v\d+\.\d+\.\d+))\) - \d{4}-\d{2}-\d{2}\r?$/m;
const changes = /^\[Changes\]\[(v\d+\.\d+\.\d+)\]\r?$/m;
const links = /^\[(v\d+\.\d+\.\d+)\]: (https:\/\/\S+)\r?$/gm;
const entry = /^- \S/m;
const unreleased = /^# Unreleased[ \t]*\r?$/m;

/** @param {string} content */
export function changelogSections(content) {
	const found = [...content.matchAll(anchors)];
	const firstLink = [...content.matchAll(links)][0]?.index;
	return found.map((anchor, index) => {
		let end = found[index + 1]?.index ?? content.length;
		if (firstLink !== undefined && firstLink > anchor.index && firstLink < end) end = firstLink;
		return { version: anchor[1], body: content.slice(anchor.index + anchor[0].length, end) };
	});
}

/** @param {string} content */
export function validateChangelog(content) {
	const sections = changelogSections(content);
	if (!sections.length) throw new Error('CHANGELOG.md declares no version section');
	/** @type {Set<string>} */
	const seen = new Set();
	for (const section of sections) {
		if (seen.has(section.version)) throw new Error(`CHANGELOG.md declares ${section.version} twice`);
		seen.add(section.version);
		const title = heading.exec(section.body);
		if (!title || title[1] !== section.version || title[3] !== section.version) {
			throw new Error(`CHANGELOG.md: ${section.version} needs a matching release heading and URL`);
		}
		if (!entry.test(section.body)) throw new Error(`CHANGELOG.md: ${section.version} describes no change`);
		if (changes.exec(section.body)?.[1] !== section.version) {
			throw new Error(`CHANGELOG.md: ${section.version} needs a matching [Changes] link`);
		}
	}
	/** @type {Set<string>} */
	const defined = new Set();
	for (const link of content.matchAll(links)) {
		const version = link[1];
		if (defined.has(version)) throw new Error(`CHANGELOG.md defines the ${version} link twice`);
		if (!seen.has(version)) throw new Error(`CHANGELOG.md defines ${version} without a version section`);
		if (!link[2].endsWith(`/${version}`) && !link[2].endsWith(`...${version}`)) {
			throw new Error(`CHANGELOG.md: the ${version} link ends at another version`);
		}
		defined.add(version);
	}
	for (const version of seen) {
		if (!defined.has(version)) throw new Error(`CHANGELOG.md: ${version} has no [Changes] link definition`);
	}
}

/** @param {string} content */
export function unreleasedEntries(content) {
	const lines = content.split(/\r?\n/);
	const start = lines.findIndex(line => line.trimEnd() === '# Unreleased');
	if (start === -1) throw new Error('CHANGELOG.md has no "# Unreleased" heading');
	const entries = [];
	for (const line of lines.slice(start + 1)) {
		if (line.startsWith('#')) break;
		if (entry.test(line)) entries.push(line.trimEnd());
	}
	return entries;
}

/** @param {string} content @param {string} tag */
export function changelogReleaseNotes(content, tag) {
	validateChangelog(content);
	const section = changelogSections(content).find(section => section.version === tag);
	if (section) {
		const title = heading.exec(section.body);
		let body = section.body.slice(title ? title.index + title[0].length : 0);
		const changesLink = changes.exec(body);
		if (changesLink) body = body.slice(0, changesLink.index);
		return `${body.replaceAll('\r\n', '\n').trim()}\n`;
	}
	const entries = unreleasedEntries(content);
	if (!entries.length) throw new Error(`CHANGELOG.md has no ${tag} section or Unreleased entries`);
	return `${entries.join('\n')}\n`;
}

/** @param {string} content @param {string} tag @param {string} date */
export function prepareChangelog(content, tag, date) {
	if (!/^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(tag)) throw new Error('Invalid changelog version');
	if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) throw new Error('Invalid changelog date');
	if (changelogSections(content).some(section => section.version === tag)) {
		validateChangelog(content);
		return content;
	}
	const start = unreleased.exec(content);
	if (!start) throw new Error('CHANGELOG.md has no "# Unreleased" heading');
	const offset = start.index + start[0].length;
	const tail = content.slice(offset);
	const nextAnchor = [...tail.matchAll(anchors)][0];
	const nextLink = [...tail.matchAll(links)][0];
	const end = offset + (nextAnchor?.index ?? nextLink?.index ?? tail.length);
	const entries = content.slice(offset, end).trim();
	if (!entry.test(entries)) throw new Error(`CHANGELOG.md has no Unreleased entries for ${tag}`);
	const previous = changelogSections(content)[0]?.version;
	const repository = 'https://github.com/kjanat/actionlint';
	const url = previous ? `${repository}/compare/${previous}...${tag}` : `${repository}/tree/${tag}`;
	const section =
		`\n\n<a id="${tag}"></a>\n\n## [${tag}](${repository}/releases/tag/${tag}) - ${date}\n\n${entries}\n\n[Changes][${tag}]\n\n`;
	const rest = content.slice(end);
	const definitions = [...rest.matchAll(links)][0];
	const definition = `[${tag}]: ${url}\n`;
	const suffix = definitions
		? rest.slice(0, definitions.index) + definition + rest.slice(definitions.index)
		: `${rest.replace(/^\n+/, '')}\n${definition}`;
	const updated = content.slice(0, offset) + section + suffix;
	validateChangelog(updated);
	return updated;
}
