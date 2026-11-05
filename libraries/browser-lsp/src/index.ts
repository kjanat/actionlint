import {
	type CodeAction,
	type CodeActionParams,
	type Command,
	type CompletionItem,
	type CompletionList,
	type CompletionParams,
	DidChangeTextDocumentNotification,
	DidCloseTextDocumentNotification,
	DidOpenTextDocumentNotification,
	ExitNotification,
	InitializedNotification,
	InitializeRequest,
	type InitializeResult,
	LogMessageNotification,
	type ProtocolConnection,
	PublishDiagnosticsNotification,
	type PublishDiagnosticsParams,
	ShutdownRequest,
} from 'vscode-languageserver-protocol';
import {
	BrowserMessageReader,
	BrowserMessageWriter,
	CodeActionRequest,
	CompletionRequest,
	createProtocolConnection,
} from 'vscode-languageserver-protocol/browser';

import { type BrowserHost, provideBrowserHost } from '#host';

export type {
	CodeAction,
	CompletionItem,
	Diagnostic,
	Position,
	PublishDiagnosticsParams,
	Range,
	TextEdit,
	WorkspaceEdit,
} from 'vscode-languageserver-protocol';

export { type BrowserHost, provideBrowserHost, ReadFileRequest, RunToolRequest } from '#host';

export interface BrowserLanguageClient {
	connection: ProtocolConnection;
	initialize(): Promise<InitializeResult>;
	open(uri: string, text: string, version: number): Promise<void>;
	change(uri: string, text: string, version: number): Promise<void>;
	close(uri: string): Promise<void>;
	complete(params: CompletionParams): Promise<CompletionItem[] | CompletionList | null>;
	codeActions(params: CodeActionParams): Promise<(CodeAction | Command)[] | null>;
	stop(): Promise<void>;
	/** Terminate immediately, including during startup or a failed connection. */
	dispose(): void;
}

export interface BrowserClientOptions {
	/** Absolute workspace URI. Virtual schemes such as vscode-vfs are supported. */
	rootURI: string;
	/** Bundled tool IDs whose commands the host can run. Omission disables external tools. */
	tools?: readonly string[];
	/** Receive versioned diagnostics exactly as published by the language server. */
	onDiagnostics(params: PublishDiagnosticsParams): void;
	/** Display operational failures separately from source diagnostics. */
	onError(message: string): void;
}

/** Connect to the packaged worker using Microsoft's browser LSP transport.
 *
 * @example
 * ```ts
 * const client = createBrowserLanguageClient(worker, {
 *   async readFile(uri) { return files.get(uri) ?? null },
 * }, {
 *   rootURI: 'file:///workspace',
 *   onDiagnostics: showDiagnostics,
 *   onError: console.error,
 * })
 * await client.initialize()
 * await client.open('file:///workspace/.github/workflows/ci.yml', yaml, 1)
 * ```
 */
export function createBrowserLanguageClient(
	worker: Worker,
	host: BrowserHost,
	options: BrowserClientOptions,
): BrowserLanguageClient {
	const connection = createProtocolConnection(new BrowserMessageReader(worker), new BrowserMessageWriter(worker));
	const operations = provideBrowserHost(connection, host);
	let initialized = false;
	let disposed = false;
	function dispose(): void {
		if (disposed) return;
		disposed = true;
		operations.dispose();
		connection.dispose();
		worker.removeEventListener('error', workerError);
		worker.terminate();
	}
	connection.onNotification(PublishDiagnosticsNotification.type, options.onDiagnostics);
	connection.onNotification(LogMessageNotification.type, ({ type, message }) => {
		if (type === 1) options.onError(message);
	});
	const workerError = (event: ErrorEvent) => {
		options.onError(event.message || 'Language server worker failed');
		dispose();
	};
	worker.addEventListener('error', workerError);
	connection.listen();
	return {
		connection,
		async initialize(): Promise<InitializeResult> {
			try {
				const result = await connection.sendRequest(InitializeRequest.type, {
					processId: null,
					clientInfo: { name: 'actionlint browser client' },
					rootUri: options.rootURI,
					capabilities: {
						general: { positionEncodings: ['utf-16'] },
						textDocument: {
							publishDiagnostics: { relatedInformation: true, versionSupport: true, codeDescriptionSupport: true },
							completion: { completionItem: { snippetSupport: false } },
							codeAction: { codeActionLiteralSupport: { codeActionKind: { valueSet: ['quickfix'] } } },
						},
						workspace: { workspaceEdit: { documentChanges: true } },
					},
					initializationOptions: { tools: options.tools ?? [] },
				});
				await connection.sendNotification(InitializedNotification.type, {});
				initialized = true;
				return result;
			} catch (error) {
				dispose();
				throw error;
			}
		},
		open(uri: string, text: string, version: number) {
			return connection.sendNotification(DidOpenTextDocumentNotification.type, {
				textDocument: { uri, languageId: 'yaml', text, version },
			});
		},
		change(uri: string, text: string, version: number) {
			return connection.sendNotification(DidChangeTextDocumentNotification.type, {
				textDocument: { uri, version },
				contentChanges: [{ text }],
			});
		},
		close(uri: string) {
			return connection.sendNotification(DidCloseTextDocumentNotification.type, { textDocument: { uri } });
		},
		complete(params: CompletionParams) {
			return connection.sendRequest(CompletionRequest.type, params);
		},
		codeActions(params: CodeActionParams) {
			return connection.sendRequest(CodeActionRequest.type, params);
		},
		async stop(): Promise<void> {
			try {
				if (!initialized || disposed) return;
				await connection.sendRequest(ShutdownRequest.type);
				await connection.sendNotification(ExitNotification.type);
			} finally {
				dispose();
			}
		},
		dispose,
	};
}
