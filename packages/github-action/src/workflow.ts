import { randomUUID } from 'node:crypto';
import { appendFile } from 'node:fs/promises';

export const commandEscape = (value: string): string =>
	value
		.replaceAll('%', '%25')
		.replaceAll('\r', '%0D')
		.replaceAll('\n', '%0A');

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
