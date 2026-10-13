import { crc32, deflateRawSync, gunzipSync, inflateRawSync } from 'node:zlib';

export interface ZipEntry {
	path: string;
	data: Buffer;
	mode: 0o644 | 0o755;
}

const dosDate = (1 << 5) | 1;

export function writeZip(entries: readonly ZipEntry[]): Buffer {
	const locals: Buffer[] = [];
	const centrals: Buffer[] = [];
	let offset = 0;
	for (const entry of entries) {
		const name = Buffer.from(entry.path, 'utf8');
		const compressed = deflateRawSync(entry.data, { level: 9 });
		const checksum = crc32(entry.data);

		const local = Buffer.alloc(30);
		local.writeUInt32LE(0x04034b50, 0);
		local.writeUInt16LE(20, 4);
		local.writeUInt16LE(0x0800, 6);
		local.writeUInt16LE(8, 8);
		local.writeUInt16LE(dosDate, 12);
		local.writeUInt32LE(checksum, 14);
		local.writeUInt32LE(compressed.length, 18);
		local.writeUInt32LE(entry.data.length, 22);
		local.writeUInt16LE(name.length, 26);

		const central = Buffer.alloc(46);
		central.writeUInt32LE(0x02014b50, 0);
		central.writeUInt16LE((3 << 8) | 20, 4);
		central.writeUInt16LE(20, 6);
		central.writeUInt16LE(0x0800, 8);
		central.writeUInt16LE(8, 10);
		central.writeUInt16LE(dosDate, 14);
		central.writeUInt32LE(checksum, 16);
		central.writeUInt32LE(compressed.length, 20);
		central.writeUInt32LE(entry.data.length, 24);
		central.writeUInt16LE(name.length, 28);
		central.writeUInt32LE((0o100000 | entry.mode) * 0x10000, 38);
		central.writeUInt32LE(offset, 42);

		locals.push(local, name, compressed);
		centrals.push(central, name);
		offset += local.length + name.length + compressed.length;
	}
	const directory = Buffer.concat(centrals);
	const end = Buffer.alloc(22);
	end.writeUInt32LE(0x06054b50, 0);
	end.writeUInt16LE(entries.length, 8);
	end.writeUInt16LE(entries.length, 10);
	end.writeUInt32LE(directory.length, 12);
	end.writeUInt32LE(offset, 16);
	return Buffer.concat([...locals, directory, end]);
}

export function readZip(archive: Buffer): Map<string, Buffer> {
	const end = archive.lastIndexOf(Buffer.from([0x50, 0x4b, 0x05, 0x06]));
	if (end < 0) throw new Error('Zip archive has no end of central directory record');
	const count = archive.readUInt16LE(end + 10);
	let cursor = archive.readUInt32LE(end + 16);
	const files = new Map<string, Buffer>();
	for (let index = 0; index < count; index++) {
		const method = archive.readUInt16LE(cursor + 10);
		const size = archive.readUInt32LE(cursor + 20);
		const nameLength = archive.readUInt16LE(cursor + 28);
		const local = archive.readUInt32LE(cursor + 42);
		const name = archive.toString('utf8', cursor + 46, cursor + 46 + nameLength);
		cursor += 46 + nameLength + archive.readUInt16LE(cursor + 30) + archive.readUInt16LE(cursor + 32);
		const start = local + 30 + archive.readUInt16LE(local + 26) + archive.readUInt16LE(local + 28);
		const raw = archive.subarray(start, start + size);
		if (method === 0) files.set(name, raw);
		else if (method === 8) files.set(name, inflateRawSync(raw));
		else throw new Error(`Zip entry ${name} uses unsupported compression method ${method}`);
	}
	return files;
}

export function readTarGz(archive: Buffer): Map<string, Buffer> {
	const tar = gunzipSync(archive);
	const files = new Map<string, Buffer>();
	const field = (header: Buffer, start: number, length: number) =>
		header.toString('utf8', start, start + length).replace(/\0.*$/s, '');
	for (let offset = 0; offset + 512 <= tar.length;) {
		const header = tar.subarray(offset, offset + 512);
		if (header.every(byte => byte === 0)) break;
		const size = Number.parseInt(field(header, 124, 12).trim(), 8);
		const data = offset + 512;
		if (field(header, 156, 1) === '0') files.set(field(header, 0, 100), tar.subarray(data, data + size));
		offset = data + Math.ceil(size / 512) * 512;
	}
	return files;
}
