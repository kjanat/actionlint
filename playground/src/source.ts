import * as pako from 'pako';

export const maxSourceBytes = 16 * 1024 * 1024;

function decode(chunks: Uint8Array[], size: number): string {
	const bytes = new Uint8Array(size);
	let offset = 0;
	for (const chunk of chunks) {
		bytes.set(chunk, offset);
		offset += chunk.length;
	}
	return new TextDecoder().decode(bytes);
}

export function inflateSource(compressed: Uint8Array): string {
	const inflator = new pako.Inflate();
	const chunks: Uint8Array[] = [];
	let size = 0;
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
