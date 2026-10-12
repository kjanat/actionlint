import stringWidth from 'string-width';

import type { ActionResult, Diagnostic } from '#result';
import type { Environment } from '#runtime';

function markdown(value: string): string {
	return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
		.replaceAll(/([\\`*_{}[\]()#+.!|~-])/g, '\\$1');
}

function message(value: string): string {
	return value.split(/(https?:\/\/[^\s<>]+)/g).map((part, index) => {
		if (index % 2 === 0) return markdown(part);
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
		return `<${url.replaceAll('&', '&amp;').replaceAll('"', '%22')}>${markdown(part.slice(url.length))}`;
	}).join('');
}

function fence(value: string): string {
	const longest = Math.max(2, ...Array.from(value.matchAll(/`+/g), (match) => match[0].length));
	const delimiter = '`'.repeat(longest + 1);
	return `${delimiter}text\n${value}\n${delimiter}\n\n`;
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
	let text = `## ${markdown(title(environment))}\n\n### Summary\n\n`;
	text += `- **Result:** ${status(result)}\n`;
	if (result.completed && result.file_count !== null) text += `- **Workflows:** ${result.file_count}\n`;
	text += '\n';
	if (result.error) text += `### Failure\n\n${markdown(failureMessage(result.error, environment))}\n\n`;
	if (!result.completed) {
		text += result.diagnostics.length
			? `Partial results: ${counts(result)}. Analysis stopped before completion; further findings may exist.\n\n`
			: 'No complete lint result is available.\n\n';
	}
	if (result.diagnostics.length) text += '### Findings\n\n';
	for (const diagnostic of result.diagnostics.slice(0, 50)) {
		text += `${message(diagnostic.message.slice(0, 1000))}\n\n`;
		text += `${markdown(diagnostic.path)}:${diagnostic.start.line}:${diagnostic.start.column} (${
			markdown(level(diagnostic))
		}, ${markdown(diagnostic.code ? `${diagnostic.rule}/${diagnostic.code}` : diagnostic.rule)})\n\n`;
		if (diagnostic.snippet) text += fence(diagnosticSnippet(diagnostic));
	}
	if (result.diagnostics.length > 50) {
		text += 'Showing the first 50 findings; the persisted result contains all findings.\n\n';
	}
	for (const hint of result.hints) text += `${markdown(hint.slice(0, 1000))}\n\n`;
	return text;
}
