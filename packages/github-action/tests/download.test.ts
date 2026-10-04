import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer, type Server } from 'node:http';
import { connect, type Socket } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { download, downloadVerified } from '#download';

async function listen(server: Server): Promise<number> {
	server.listen(0, '127.0.0.1');
	await once(server, 'listening');
	const address = server.address();
	assert.ok(address && typeof address === 'object');
	return address.port;
}

test(
	'native download follows redirects through the runner proxy and honors NO_PROXY',
	{ timeout: 5_000 },
	async (t) => {
		const directory = await mkdtemp(join(tmpdir(), 'actionlint-proxy-test-'));
		const variables = ['HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY', 'http_proxy', 'https_proxy', 'no_proxy'];
		const original = new Map(variables.map((name) => [name, process.env[name]]));
		const sockets = new Set<Socket>();
		const requests: string[] = [];
		const tunnels: string[] = [];
		const body = 'archive bytes from the proxy-only origin';
		const origin = createServer((request, response) => {
			requests.push(request.url ?? '');
			if (request.url === '/redirect') {
				response.writeHead(302, { location: '/archive' });
				response.end();
			} else if (request.url === '/missing') {
				response.writeHead(404);
				response.end('missing');
			} else {
				response.end(body);
			}
		});
		const proxy = createServer();
		for (const server of [origin, proxy]) {
			server.on('connection', (socket) => {
				sockets.add(socket);
				socket.once('close', () => sockets.delete(socket));
			});
		}
		t.after(async () => {
			for (const [name, value] of original) {
				if (value === undefined) delete process.env[name];
				else process.env[name] = value;
			}
			for (const socket of sockets) socket.destroy();
			await Promise.all([origin, proxy].map((server) =>
				new Promise<void>((resolve, reject) => {
					if (!server.listening) return resolve();
					server.close((error) => error ? reject(error) : resolve());
				})
			));
			await rm(directory, { recursive: true, force: true });
		});
		const originPort = await listen(origin);
		proxy.on('connect', (request, socket, head) => {
			tunnels.push(request.url ?? '');
			const upstream = connect(originPort, '127.0.0.1');
			sockets.add(upstream);
			upstream.once('close', () => sockets.delete(upstream));
			upstream.once('error', () => socket.destroy());
			socket.once('error', () => upstream.destroy());
			upstream.once('connect', () => {
				socket.write('HTTP/1.1 200 Connection Established\r\n\r\n');
				if (head.length) upstream.write(head);
				socket.pipe(upstream).pipe(socket);
			});
		});
		const proxyPort = await listen(proxy);
		for (const name of variables) delete process.env[name];
		process.env.HTTP_PROXY = `http://127.0.0.1:${proxyPort}`;
		const digest = createHash('sha256').update(body).digest('hex');
		const proxied = await downloadVerified(
			download,
			'http://proxy-only.invalid/redirect',
			join(directory, 'proxied'),
			digest,
		);
		assert.equal(await readFile(proxied, 'utf8'), body);
		assert.deepEqual(requests, ['/redirect', '/archive']);
		assert.ok(tunnels.length > 0);
		assert.ok(tunnels.every((target) => target === 'proxy-only.invalid:80'));
		await assert.rejects(download('http://proxy-only.invalid/missing', join(directory, 'missing')), /HTTP 404/);
		await assert.rejects(readFile(join(directory, 'missing')), { code: 'ENOENT' });
		const beforeBypass = tunnels.length;
		process.env.NO_PROXY = '127.0.0.1';
		const direct = await download(`http://127.0.0.1:${originPort}/archive`, join(directory, 'direct'));
		assert.equal(await readFile(direct, 'utf8'), body);
		assert.equal(tunnels.length, beforeBypass);
	},
);

test('downloaded archives are accepted only after checksum verification', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-download-test-'));
	try {
		const destination = join(directory, 'archive.zip');
		const body = 'verified archive bytes';
		const download = async (_url: string, path: string): Promise<string> => {
			await writeFile(path, body);
			return path;
		};
		const digest = createHash('sha256').update(body).digest('hex');
		const downloaded = await downloadVerified(download, 'https://example.invalid/archive.zip', destination, digest);
		assert.equal(await readFile(downloaded, 'utf8'), body);
		await assert.rejects(
			downloadVerified(download, 'https://example.invalid/archive.zip', destination, '0'.repeat(64)),
			/SHA-256 checksum mismatch/,
		);
		await assert.rejects(readFile(destination), { code: 'ENOENT' });
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

test('native download rejects HTTP failures and preserves existing files', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'actionlint-http-test-'));
	try {
		const destination = join(directory, 'archive');
		await assert.rejects(
			download('https://example.invalid/archive', destination, async () => new Response('missing', { status: 404 })),
			/HTTP 404/,
		);
		await assert.rejects(readFile(destination), { code: 'ENOENT' });
		await download('https://example.invalid/archive', destination, async () => new Response('verified bytes'));
		assert.equal(await readFile(destination, 'utf8'), 'verified bytes');
		await assert.rejects(
			download('https://example.invalid/archive', destination, async () => new Response('replacement')),
			{ code: 'EEXIST' },
		);
		assert.equal(await readFile(destination, 'utf8'), 'verified bytes');
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});
