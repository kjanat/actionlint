import * as pako from 'pako';

export const maxSourceBytes = 16 * 1024 * 1024;

// zlib's compressBound in compress.c bounds the deflate output for maxSourceBytes.
const maxCompressedBytes = maxSourceBytes + (maxSourceBytes >>> 12) + (maxSourceBytes >>> 14) + (maxSourceBytes >>> 25)
	+ 13;

export const maxEncodedLength = 4 * Math.ceil(maxCompressedBytes / 3);

function decode(chunks: Uint8Array[], size: number): string {
	const bytes = new Uint8Array(size);
	let offset = 0;
	for (const chunk of chunks) {
		bytes.set(chunk, offset);
		offset += chunk.length;
	}
	return new TextDecoder().decode(bytes);
}

// Spreading the whole array into String.fromCharCode throws RangeError past about 128 KiB.
function toBase64(bytes: Uint8Array): string {
	let s = '';
	for (const b of bytes) {
		s += String.fromCharCode(b);
	}
	return btoa(s);
}

// Chromium refuses URLs longer than 2 MiB.
export const maxURLLength = 2 * 1024 * 1024;

function sourceBytes(source: string, origin: string): Uint8Array {
	const bytes = new TextEncoder().encode(source);
	if (bytes.length > maxSourceBytes) {
		throw new Error(`The ${origin} is larger than ${maxSourceBytes} bytes`);
	}
	return bytes;
}

export function querySource(source: string): string {
	sourceBytes(source, 'query source');
	return source;
}

export function shareURL(base: string, source: string): string {
	const url = `${base}#${toBase64(pako.deflate(sourceBytes(source, 'source')))}`;
	if (url.length > maxURLLength) {
		throw new Error(`The shared URL is longer than ${maxURLLength} characters`);
	}
	return url;
}

export function sharedSource(encoded: string): string {
	if (encoded.length > maxEncodedLength) {
		throw new Error(`The shared source is longer than ${maxEncodedLength} base64 characters`);
	}
	return inflateSource(Uint8Array.from(atob(encoded), c => c.charCodeAt(0)));
}

function inflateSource(compressed: Uint8Array): string {
	const inflator = new pako.Inflate();
	const chunks: Uint8Array[] = [];
	let size = 0;
	let stream: pako.ZStream | undefined;
	inflator.onStart = strm => {
		stream = strm;
	};
	inflator.onData = chunk => {
		size += chunk.length;
		if (size > maxSourceBytes) {
			throw new Error(`The shared source inflates past ${maxSourceBytes} bytes`);
		}
		chunks.push(chunk);
	};
	inflator.push(compressed, true);
	if (inflator.err !== 0 || !inflator.ended) {
		throw new Error(`The shared source is not a complete zlib stream: ${inflator.msg}`);
	}
	const unread = stream?.avail_in ?? 0;
	if (unread !== 0) {
		throw new Error(`The shared source has ${unread} bytes after its zlib stream`);
	}
	return decode(chunks, size);
}

export async function readSource(response: Response): Promise<string> {
	if (response.body === null) {
		return '';
	}
	const reader = response.body.getReader();
	const chunks: Uint8Array[] = [];
	let size = 0;
	for (;;) {
		const { done, value } = await reader.read();
		if (done) {
			return decode(chunks, size);
		}
		size += value.length;
		if (size > maxSourceBytes) {
			await reader.cancel();
			throw new Error(`The remote source is larger than ${maxSourceBytes} bytes`);
		}
		chunks.push(value);
	}
}
