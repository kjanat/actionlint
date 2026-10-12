import { isAbsolute, relative, resolve, sep } from 'node:path';

import stringWidth from 'string-width';

import type { ActionResult, Diagnostic } from '#result';
import type { Environment } from '#runtime';

function html(value: string): string {
	return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
		.replaceAll('"', '&quot;').replaceAll("'", '&#39;').replaceAll(/\r\n?|\n/g, '&#10;');
}

function message(value: string): string {
	return value.split(/(https?:\/\/[^\s<>]+)/g).map((part, index) => {
		if (index % 2 === 0) return html(part);
		let url = part;
		const pairs: Record<string, string> = { ')': '(', ']': '[', '}': '{' };
		while (url) {
			const last = url.at(-1) || '';
			const open = pairs[last];
			if (/[.,;:!?"']/.test(last) || (open && url.split(last).length > url.split(open).length)) {
				url = url.slice(0, -1);
			} else {
				break;
			}
		}
		return `<a href="${html(url)}">Documentation</a>${html(part.slice(url.length))}`;
	}).join('');
}

function sourceLink(diagnostic: Diagnostic, environment: Environment): string {
	const label = `<code>${html(diagnostic.path)}:${diagnostic.start.line}:${diagnostic.start.column}</code>`;
	const repository = environment.GITHUB_REPOSITORY || '';
	const sha = environment.GITHUB_SHA || '';
	const server = environment.GITHUB_SERVER_URL || 'https://github.com';
	if (!/^[\w-][\w.-]*\/[\w-][\w.-]*$/.test(repository) || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(sha)) return label;
	let origin: URL;
	try {
		origin = new URL(server);
	} catch {
		return label;
	}
	if (origin.protocol !== 'https:' || origin.username || origin.password) return label;
	const workspace = resolve(environment.GITHUB_WORKSPACE || '.');
	const directory = resolve(workspace, environment['INPUT_WORKING-DIRECTORY'] || '.');
	const path = relative(workspace, resolve(directory, diagnostic.path));
	if (!path || isAbsolute(path) || path === '..' || path.startsWith(`..${sep}`)) return label;
	const encoded = path.split(sep).map(encodeURIComponent).join('/');
	const end = diagnostic.end.line > diagnostic.start.line && diagnostic.end.column === 1
		? diagnostic.end.line - 1
		: diagnostic.end.line;
	const range = `#L${diagnostic.start.line}${end > diagnostic.start.line ? `-L${end}` : ''}`;
	return `<a href="${html(`${origin.origin}/${repository}/blob/${sha}/${encoded}${range}`)}">${label}</a>`;
}

function level(diagnostic: Diagnostic): string {
	const severity = diagnostic.severity || 'error';
	return ['warning', 'info', 'style'].includes(severity) ? severity : 'error';
}

function counts(result: ActionResult): string {
	return ['error', 'warning', 'info', 'style'].flatMap((severity) => {
		const count = result.diagnostics.filter((diagnostic) => level(diagnostic) === severity).length;
		const name = severity === 'info' ? 'notice' : severity === 'style' ? 'style finding' : severity;
		return count ? [`${count} ${name}${count === 1 ? '' : 's'}`] : [];
	}).join(', ');
}

function title(environment: Environment): string {
	const label = (environment['INPUT_SUMMARY-TITLE']?.trim() || environment.GITHUB_ACTION || '').replaceAll(
		/[\r\n]/g,
		' ',
	).slice(0, 120);
	return label ? `actionlint (${label})` : 'actionlint';
}

function status(result: ActionResult): string {
	if (!result.completed) {
		return result.diagnostics.length || result.file_count ? 'Analysis incomplete' : 'Could not start analysis';
	}
	if (result.file_count === 0 && !result.diagnostics.length) return 'No workflows selected';
	return counts(result) || 'No findings';
}

export function reportStatus(result: ActionResult, environment: Environment = {}): string {
	return `${title(environment)}: ${status(result)}`;
}

function failureMessage(message: string, environment: Environment): string {
	const missing =
		/^could not read config file ("(?:[^"\\]|\\.)*"): .*: (?:no such file or directory|The system cannot find the (?:file|path) specified\.)$/s
			.exec(message);
	if (missing?.[1]) {
		let path: string;
		try {
			path = JSON.parse(missing[1]) as string;
		} catch {
			return message.slice(0, 2000);
		}
		const workspace = environment.GITHUB_WORKSPACE?.replace(/[\\/]+$/, '');
		if (workspace && (path.startsWith(`${workspace}/`) || path.startsWith(`${workspace}\\`))) {
			path = path.slice(workspace.length + 1);
		}
		return `Configuration file not found: ${path}\n\nCheck the configured path and ensure the file exists in the checkout.`;
	}
	return message.slice(0, 2000);
}

// Source columns count code points; caret padding uses displayed character widths.
export function diagnosticSnippet(diagnostic: Diagnostic): string {
	const source = Array.from((diagnostic.snippet || '').split('\n')[0] || '');
	const offset = Math.max(0, Math.min(source.length, diagnostic.start.column - 1) - 80);
	const line = source.slice(offset, offset + 240);
	const start = Math.min(line.length, diagnostic.start.column - 1 - offset);
	const end = diagnostic.end.line > diagnostic.start.line
		? line.length
		: Math.min(line.length, diagnostic.end.column - 1 - offset);
	const expand = (characters: string[]): string => characters.join('').replaceAll('\t', '    ');
	const prefix = (offset ? '…' : '') + expand(line.slice(0, start));
	const width = Math.max(1, stringWidth(expand(line.slice(start, end))));
	const number = String(diagnostic.start.line);
	const preview = (offset ? '…' : '') + expand(line) + (source.length > offset + line.length ? '…' : '');
	return `${number} | ${preview}\n${' '.repeat(number.length)} | ${' '.repeat(stringWidth(prefix))}^${
		'~'.repeat(width - 1)
	}`;
}

export function summary(result: ActionResult, environment: Environment = {}): string {
	let text = `<h2>${html(title(environment))}</h2>\n\n`;
	text += `<p><strong>${status(result)}`;
	if (result.completed && result.file_count) {
		text += ` in ${result.file_count} workflow${result.file_count === 1 ? '' : 's'}`;
	}
	text += '</strong></p>\n\n';
	if (result.error) text += `<p>${html(failureMessage(result.error, environment)).replaceAll('&#10;', '<br>')}</p>\n\n`;
	if (!result.completed) {
		text += result.diagnostics.length
			? `Partial results: ${counts(result)}. Analysis stopped before completion; further findings may exist.\n\n`
			: 'No complete lint result is available.\n\n';
	}
	for (const diagnostic of result.diagnostics.slice(0, 50)) {
		const severity = level(diagnostic) === 'info' ? 'notice' : level(diagnostic);
		const rule = diagnostic.code ? `${diagnostic.rule}/${diagnostic.code}` : diagnostic.rule;
		text += `<p><strong>${html(severity)}</strong> <code>${html(rule)}</code><br>${
			sourceLink(diagnostic, environment)
		}</p>\n\n`;
		text += `<p>${message(diagnostic.message.slice(0, 1000)).replaceAll('&#10;', '<br>')}</p>\n\n`;
		if (diagnostic.snippet) {
			const source = diagnosticSnippet(diagnostic).split('\n')[0]?.replace(/^\d+ \| /, '') || '';
			text += `<pre lang="yaml">${html(source)}</pre>\n\n`;
		}
	}
	if (result.diagnostics.length > 50) {
		text += 'Showing the first 50 findings; the persisted result contains all findings.\n\n';
	}
	for (const hint of result.hints) text += `<p>${html(hint.slice(0, 1000))}</p>\n\n`;
	return text;
}
