import { randomUUID } from 'node:crypto';
import { appendFile, readFile } from 'node:fs/promises';

export const commandEscape = (value: string): string =>
	value
		.replaceAll('%', '%25')
		.replaceAll('\r', '%0D')
		.replaceAll('\n', '%0A');

// Read the multiline file-command format written by the native Action.
export async function readOutputs(path: string): Promise<Record<string, string>> {
	const lines = (await readFile(path, 'utf8')).split(/\r?\n/);
	const values: Record<string, string> = {};
	for (let i = 0; i < lines.length; i++) {
		const line = lines[i];
		if (!line) continue;
		const match = /^([a-z][a-z0-9-]*)<<(.+)$/.exec(line);
		const name = match?.[1], delimiter = match?.[2];
		if (!name || !delimiter) throw new Error('Invalid native Action output');
		const end = lines.indexOf(delimiter, i + 1);
		if (end < 0) throw new Error(`Unterminated native Action output: ${name}`);
		values[name] = lines.slice(i + 1, end).join('\n');
		i = end;
	}
	return values;
}

export async function writeOutputs(path: string | undefined, values: Readonly<Record<string, string>>): Promise<void> {
	if (!path) return;
	let content = '';
	for (const [name, value] of Object.entries(values)) {
		if (!/^[a-z][a-z0-9-]*$/.test(name)) throw new Error(`Invalid output name: ${name}`);
		let delimiter = randomUUID();
		while (value.includes(delimiter)) delimiter = randomUUID();
		content += `${name}<<${delimiter}\n${value}\n${delimiter}\n`;
	}
	await appendFile(path, content);
}
