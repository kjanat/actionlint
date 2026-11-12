import './wasm_exec.js';

import {
	BrowserMessageReader,
	BrowserMessageWriter,
	type CancellationToken,
	CancellationTokenSource,
	createMessageConnection,
	Message,
	ResponseError,
} from 'vscode-jsonrpc/browser';

declare const self: DedicatedWorkerGlobalScope;
declare class Go {
	importObject: WebAssembly.Imports;
	run(instance: WebAssembly.Instance): Promise<void>;
}

declare global {
	var actionlintReceiveMessage: ((text: string) => boolean) | undefined;
	var actionlintHostRequest: (id: string, method: string, params: string) => Promise<unknown>;
	var actionlintCancelHostRequest: (id: string) => void;
	var actionlintSendMessage: (text: string) => void;
	var actionlintReady: () => void;
	var actionlintServerError: (message: string) => void;
}

const connection = createMessageConnection(new BrowserMessageReader(self), new BrowserMessageWriter(self));
const requests = new Map<number, { resolve(value: unknown): void; reject(error: Error): void }>();
const hostRequests = new Map<string, CancellationTokenSource>();
let sequence = 0;
const ready = Promise.withResolvers<{ ok: true } | { ok: false; error: Error }>();

function isMessage(value: unknown): value is Message {
	return typeof value === 'object' && value !== null && 'jsonrpc' in value && value.jsonrpc === '2.0';
}

function send(method: string, params: unknown, id?: number): void {
	const message = { jsonrpc: '2.0', method, params, ...(id === undefined ? {} : { id }) };
	if (!globalThis.actionlintReceiveMessage?.(JSON.stringify(message))) {
		throw new Error('The language server input queue is unavailable');
	}
}

globalThis.actionlintReady = () => ready.resolve({ ok: true });
globalThis.actionlintServerError = message => {
	for (const pending of requests.values()) pending.reject(new Error(message));
	requests.clear();
	void connection.sendNotification('window/logMessage', { type: 1, message });
};
globalThis.actionlintSendMessage = text => {
	const message: unknown = JSON.parse(text);
	if (!isMessage(message)) {
		throw new Error('The language server emitted an invalid JSON-RPC message');
	}
	if (Message.isResponse(message) && typeof message.id === 'number') {
		const request = requests.get(message.id);
		requests.delete(message.id);
		if (message.error) {
			request?.reject(new ResponseError(message.error.code, message.error.message, message.error.data));
		} else request?.resolve(message.result);
	} else if (Message.isNotification(message)) {
		void connection.sendNotification(message.method, message.params);
	}
};
globalThis.actionlintHostRequest = async (id, method, params) => {
	const cancellation = new CancellationTokenSource();
	hostRequests.set(id, cancellation);
	try {
		const cancelled = new Promise<never>((_resolve, reject) => {
			cancellation.token.onCancellationRequested(() => reject(new Error('Host request cancelled')));
		});
		const parameters: unknown = JSON.parse(params);
		return await Promise.race([connection.sendRequest(method, parameters, cancellation.token), cancelled]);
	} finally {
		hostRequests.delete(id);
		cancellation.dispose();
	}
};
globalThis.actionlintCancelHostRequest = id => hostRequests.get(id)?.cancel();

connection.onRequest(async (method: string, params: unknown, cancellation: CancellationToken) => {
	const startup = await ready.promise;
	if (!startup.ok) throw startup.error;
	const id = ++sequence;
	const reply = new Promise<unknown>((resolve, reject) => requests.set(id, { resolve, reject }));
	const listener = cancellation.onCancellationRequested(() => send('$/cancelRequest', { id }));
	try {
		send(method, params, id);
		return await reply;
	} finally {
		listener.dispose();
		requests.delete(id);
	}
});
connection.onNotification(async (method: string, params: unknown) => {
	const startup = await ready.promise;
	if (!startup.ok) return;
	send(method, params);
});
connection.listen();

void (async () => {
	const go = new Go();
	const response = await fetch(new URL('./server.wasm', import.meta.url));
	if (!response.ok) throw new Error(`Could not load actionlint WebAssembly: HTTP ${response.status}`);
	const bytes = await response.arrayBuffer();
	const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
	await go.run(instance);
	connection.dispose();
})().catch((error: unknown) => {
	const failure = error instanceof Error ? error : new Error(String(error));
	ready.resolve({ ok: false, error: failure });
	globalThis.actionlintServerError(failure.message);
});
