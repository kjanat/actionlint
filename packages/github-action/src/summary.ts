import { realpathSync } from 'node:fs';
import { isAbsolute, relative, resolve, sep } from 'node:path';

import stringWidth from 'string-width';

import type { ActionResult, Diagnostic } from '#result';
import type { Environment } from '#runtime';
import { sourceLinkVerifier } from '#source-links';

function html(value: string): string {
	return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
		.replaceAll('"', '&quot;').replaceAll("'", '&#39;').replaceAll(/\r\n?|\n/g, '&#10;');
}

function message(value: string): string {
	const prose = value.replace(/(?:^|\s+)see\s+(https?:\/\/[^\s<>]+)(?:\s+for (?:more )?details\.?)?\s*$/i, ' $1');
	return prose.split(/(https?:\/\/[^\s<>]+)/g).map((part, index) => {
		if (index % 2 === 0) {
			return part.split(
				/(["'](?:github|env|vars|job|jobs|steps|runner|secrets|strategy|matrix|needs|inputs)(?:\.(?:[A-Za-z_][A-Za-z0-9_-]*|\*))+["'])/g,
			)
				.map((piece, pieceIndex) =>
					pieceIndex % 2 && piece[0] === piece.at(-1)
						? `<code>${html(piece.slice(1, -1))}</code>`
						: html(piece)
				).join('');
		}
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
		return `<br><sub><a href="${html(url)}">${html(url)}</a></sub>${html(part.slice(url.length))}`;
	}).join('');
}

function sourceLink(diagnostic: Diagnostic, environment: Environment, verified: (path: string) => boolean): string {
	const label = `<code>${html(diagnostic.path)}:${diagnostic.start.line}:${diagnostic.start.column}</code>`;
	const repository = environment.GITHUB_REPOSITORY || '';
	const sha = environment.GITHUB_SHA || '';
	const server = environment.GITHUB_SERVER_URL || 'https://github.com';
	if (
		!/^[\w-][\w.-]*\/[\w.-]+$/.test(repository)
		|| repository.split('/').some(segment => segment === '.' || segment === '..')
		|| !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(sha)
	) return label;
	let origin: URL;
	try {
		origin = new URL(server);
	} catch {
		return label;
	}
	if (origin.protocol !== 'https:' || origin.username || origin.password) return label;
	let path: string;
	try {
		const configuredWorkspace = resolve(environment.GITHUB_WORKSPACE || '.');
		const workspace = realpathSync(configuredWorkspace);
		const directory = resolve(configuredWorkspace, environment['INPUT_WORKING-DIRECTORY'] || '.');
		path = relative(workspace, realpathSync(resolve(directory, diagnostic.path)));
	} catch {
		return label;
	}
	if (!path || isAbsolute(path) || path === '..' || path.startsWith(`..${sep}`)) return label;
	if (!verified(path)) return label;
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
	return result.diagnostics.length ? '' : 'No findings';
}

export function reportStatus(result: ActionResult, environment: Environment = {}): string {
	const state = status(result);
	return state ? `${title(environment)}: ${state}` : '';
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
export function diagnosticSnippet(diagnostic: Diagnostic, numbered = true): string {
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
	const caret = `${' '.repeat(stringWidth(prefix))}^${'~'.repeat(width - 1)}`;
	return numbered ? `${number} | ${preview}\n${' '.repeat(number.length)} | ${caret}` : `${preview}\n${caret}`;
}

function contextualSnippet(diagnostic: Diagnostic, lines?: string[]): string {
	if (!lines) return diagnosticSnippet(diagnostic, false);
	const first = Math.max(1, diagnostic.start.line - 2);
	const last = Math.min(lines.length, diagnostic.start.line + 2);
	const output: string[] = [];
	for (let number = first; number <= last; number++) {
		const snippet = lines[number - 1] || '';
		if (number === diagnostic.start.line) {
			output.push(diagnosticSnippet({ ...diagnostic, snippet }, false));
		} else {
			const points = Array.from(snippet);
			output.push(
				`${points.slice(0, 240).join('').replaceAll('\t', '    ')}${points.length > 240 ? '…' : ''}`,
			);
		}
	}
	return output.join('\n');
}

export function summary(
	result: ActionResult,
	environment: Environment = {},
	contexts = new Map<Diagnostic, string[]>(),
): string {
	const verified = sourceLinkVerifier(environment);
	const label = environment['INPUT_SUMMARY-TITLE']?.trim();
	let text = label ? `<p><strong>${html(label.replaceAll(/[\r\n]/g, ' ').slice(0, 120))}</strong></p>\n\n` : '';
	const state = status(result);
	if (state) text += `<p><strong>${state}</strong></p>\n\n`;
	if (result.error) text += `<p>${html(failureMessage(result.error, environment)).replaceAll('&#10;', '<br>')}</p>\n\n`;
	if (!result.completed) {
		text += result.diagnostics.length
			? 'Analysis stopped before completion; further findings may exist.\n\n'
			: 'No complete lint result is available.\n\n';
	}
	for (const diagnostic of result.diagnostics.slice(0, 50)) {
		const severity = level(diagnostic) === 'info' ? 'notice' : level(diagnostic);
		const icon = severity === 'error' ? '🔴' : severity === 'warning' ? '⚠️' : severity === 'notice' ? 'ℹ️' : '💡';
		const rule = diagnostic.code ? `${diagnostic.rule}/${diagnostic.code}` : diagnostic.rule;
		const prose = diagnostic.message.slice(0, 1000);
		text += `<p>${icon} <strong>${html(severity[0]?.toUpperCase() || '')}${html(severity.slice(1))}:</strong> ${
			message(prose).replaceAll('&#10;', '<br>')
		}</p>\n\n`;
		text += `<p>${sourceLink(diagnostic, environment, verified)} | Rule: <code>${html(rule)}</code></p>\n\n`;
		if (diagnostic.snippet || contexts.has(diagnostic)) {
			text += `<pre lang="yaml">${html(contextualSnippet(diagnostic, contexts.get(diagnostic)))}</pre>\n\n`;
		}
	}
	if (result.diagnostics.length > 50) {
		text += 'Showing the first 50 findings; the persisted result contains all findings.\n\n';
	}
	for (const hint of result.hints) text += `<p>${html(hint.slice(0, 1000))}</p>\n\n`;
	return text;
}
