import { Buffer } from 'node:buffer';
import process from 'node:process';

import type { DocumentCheck, ScriptCheck, ScriptOutput } from '#index';
import { executeDocumentCheck, executeScriptCheck } from '#index';

/**
 * Read one JSON script request from stdin and write one validated SDK response to stdout.
 * `pack` generates this entrypoint. Call it yourself only from a custom runner.
 * Write logs to stderr. Stdout carries protocol JSON only.
 * @param check Script callback; thrown errors become protocol failures.
 */
export async function serveScriptEngine(
	check: ScriptCheck,
	limits: {
		/** Maximum UTF-8 request bytes read from stdin; defaults to 8 MiB. */
		inputBytes?: number;
		/** Maximum UTF-8 serialized response bytes; defaults to 8 MiB. */
		outputBytes?: number;
	} = {},
): Promise<void> {
	const inputLimit = limits.inputBytes ?? 8 * 1024 * 1024;
	const outputLimit = limits.outputBytes ?? 8 * 1024 * 1024;
	let input: string;
	try {
		input = await readStdin(inputLimit);
	} catch (error) {
		writeJSON({ ok: false, diagnostics: [], errors: [messageOf(error)] }, outputLimit);
		return;
	}
	const output = await executeScriptCheck(check, input, outputLimit, inputLimit);
	writeJSON(output, outputLimit);
}

/**
 * Serve one complete-document request over stdin/stdout, including requested dependency facts.
 * `pack` generates this entrypoint. Stdout carries protocol JSON only.
 * @param check Document callback; thrown errors become protocol failures.
 */
export async function serveDocumentEngine(
	check: DocumentCheck,
	limits: {
		/** Maximum UTF-8 document request bytes, including facts; defaults to 8 MiB. */
		inputBytes?: number;
		/** Maximum UTF-8 serialized response bytes; defaults to 8 MiB. */
		outputBytes?: number;
	} = {},
): Promise<void> {
	const inputLimit = limits.inputBytes ?? 8 * 1024 * 1024;
	const outputLimit = limits.outputBytes ?? 8 * 1024 * 1024;
	let input: string;
	try {
		input = await readStdin(inputLimit);
	} catch (error) {
		writeJSON({ ok: false, diagnostics: [], errors: [messageOf(error)] }, outputLimit);
		return;
	}
	writeJSON(await executeDocumentCheck(check, input, outputLimit, inputLimit), outputLimit);
}

async function readStdin(limit: number): Promise<string> {
	const chunks: Buffer[] = [];
	let size = 0;
	for await (const chunk of process.stdin) {
		const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
		size += bytes.byteLength;
		if (size > limit) throw new Error(`request exceeds ${limit} byte limit`);
		chunks.push(bytes);
	}
	return Buffer.concat(chunks).toString('utf8');
}
function writeJSON(output: ScriptOutput, limit: number): void {
	const text = JSON.stringify(output);
	if (Buffer.byteLength(text) > limit) {
		process.stdout.write(
			JSON.stringify({ ok: false, diagnostics: [], errors: [`response exceeds ${limit} byte limit`] }),
		);
		return;
	}
	process.stdout.write(text);
}

function messageOf(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
