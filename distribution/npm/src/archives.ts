import { posix } from 'node:path';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import { createGunzip } from 'node:zlib';

import { Uint8ArrayReader, ZipReader } from '@zip.js/zip.js';
import { extract } from 'tar-stream';

import type { ArchiveFormat } from '#targets';

export const maximumArchiveBytes = 256 << 20;

export async function readBounded(source: AsyncIterable<unknown>, name: string): Promise<Buffer> {
	const chunks: Buffer[] = [];
	let size = 0;
	for await (const chunk of source) {
		if (!(chunk instanceof Uint8Array)) throw new Error(`${name} produced a non-binary stream chunk`);
		size += chunk.length;
		if (size >= maximumArchiveBytes) throw new Error(`${name} exceeds the ${maximumArchiveBytes} byte limit`);
		chunks.push(Buffer.from(chunk));
	}
	if (size === 0) throw new Error(`${name} is empty`);
	return Buffer.concat(chunks, size);
}

async function tarMembers(archive: Buffer, wanted: ReadonlySet<string>): Promise<Map<string, Buffer>> {
	const unpack = extract();
	const files = new Map<string, Buffer>();
	const read = async () => {
		try {
			for await (const stream of unpack) {
				const { header } = stream;
				const name = posix.basename(header.name);
				if (header.type !== 'file' || !wanted.has(name)) {
					stream.resume();
					continue;
				}
				if (files.has(name)) throw new Error(`Archive contains more than one ${name}`);
				if (header.size >= maximumArchiveBytes) {
					throw new Error(`${name} exceeds the ${maximumArchiveBytes} byte limit`);
				}
				files.set(name, await readBounded(stream, name));
			}
		} catch (error) {
			unpack.destroy(error instanceof Error ? error : new Error('Tar extraction failed', { cause: error }));
			throw error;
		}
	};
	const results = await Promise.allSettled([read(), pipeline(Readable.from([archive]), createGunzip(), unpack)]);
	for (const result of results) if (result.status === 'rejected') throw result.reason;
	return files;
}

async function zipMembers(archive: Buffer, wanted: ReadonlySet<string>): Promise<Map<string, Buffer>> {
	const reader = new ZipReader(new Uint8ArrayReader(archive), { checkCrc32: true, useWebWorkers: false });
	const files = new Map<string, Buffer>();
	try {
		for (const entry of await reader.getEntries()) {
			const name = posix.basename(entry.filename);
			if (entry.directory || !wanted.has(name)) continue;
			if (files.has(name)) throw new Error(`Archive contains more than one ${name}`);
			if (entry.uncompressedSize >= maximumArchiveBytes) {
				throw new Error(`${name} exceeds the ${maximumArchiveBytes} byte limit`);
			}
			const content = Buffer.from(await entry.arrayBuffer());
			if (!content.length) throw new Error(`${name} is empty`);
			if (content.length >= maximumArchiveBytes) {
				throw new Error(`${name} exceeds the ${maximumArchiveBytes} byte limit`);
			}
			files.set(name, content);
		}
	} finally {
		await reader.close();
	}
	return files;
}

/** Reads selected files into memory. Archive paths are never written to disk. */
export async function archiveMembers(
	archive: Buffer,
	format: ArchiveFormat,
	names: readonly string[],
): Promise<Map<string, Buffer>> {
	const wanted = new Set(names);
	const files = format === 'tar.gz' ? await tarMembers(archive, wanted) : await zipMembers(archive, wanted);
	for (const name of names) if (!files.has(name)) throw new Error(`No ${name} in the archive`);
	return files;
}
