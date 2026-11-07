import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { rm } from 'node:fs/promises';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';

import { EnvHttpProxyAgent, fetch, type RequestInit, type Response } from 'undici';

export type Download = (url: string, destination: string) => Promise<string>;
type DownloadRequest = (url: string, options: RequestInit) => Promise<Pick<Response, 'ok' | 'status' | 'body'>>;

export async function download(url: string, destination: string, request: DownloadRequest = fetch): Promise<string> {
	const dispatcher = new EnvHttpProxyAgent();
	try {
		const response = await request(url, { dispatcher, signal: AbortSignal.timeout(120_000) });
		if (!response.ok) {
			await response.body?.cancel();
			throw new Error(`Download failed: HTTP ${response.status} for ${new URL(url).pathname}`);
		}
		if (!response.body) throw new Error(`Download returned no body: ${new URL(url).pathname}`);
		const output = createWriteStream(destination, { flags: 'wx' });
		let created = false;
		output.once('open', () => {
			created = true;
		});
		try {
			await pipeline(Readable.fromWeb(response.body), output);
			return destination;
		} catch (error) {
			if (created) await rm(destination, { force: true });
			throw error;
		}
	} finally {
		await dispatcher.close();
	}
}

export async function sha256File(path: string): Promise<string> {
	const hash = createHash('sha256');
	for await (const chunk of createReadStream(path)) hash.update(chunk);
	return hash.digest('hex');
}

export async function downloadVerified(
	download: Download,
	url: string,
	destination: string,
	expected: string,
): Promise<string> {
	if (expected.length !== 64 || !/^[a-f0-9]{64}$/.test(expected)) throw new Error('Invalid expected SHA-256 checksum');
	const path = await download(url, destination);
	if (await sha256File(path) !== expected) {
		await rm(path, { force: true });
		throw new Error(`SHA-256 checksum mismatch for ${new URL(url).pathname.split('/').at(-1)}`);
	}
	return path;
}
