import type { CancellationToken, Disposable, ProtocolConnection } from 'vscode-languageserver-protocol';
import { RequestType } from 'vscode-languageserver-protocol';

/** Files come from the editor host, including VS Code's workspace.fs API. */
export const ReadFileRequest = new RequestType<{ uri: string }, string | null, void>('actionlint/readFile');

/** Tools execute in the client host; their output follows their existing provider manifest. */
export const RunToolRequest = new RequestType<
	{
		name: string;
		args: string[];
		stdin: string;
	},
	string,
	void
>('actionlint/runTool');

export interface BrowserHost {
	/** Read UTF-8 workspace text. Return null for a missing file. Do not fetch arbitrary URLs. */
	readFile(uri: string, cancellation: CancellationToken): Promise<string | null>;
	/** Run a tool explicitly named in initializationOptions.tools. Return its standard output. */
	runTool?(name: string, args: string[], stdin: string, cancellation: CancellationToken): Promise<string>;
}

/** Attach host operations to a protocol connection before opening documents. */
export function provideBrowserHost(connection: Pick<ProtocolConnection, 'onRequest'>, host: BrowserHost): Disposable {
	const files = connection.onRequest(ReadFileRequest, ({ uri }, cancellation) => host.readFile(uri, cancellation));
	const tools = connection.onRequest(RunToolRequest, ({ name, args, stdin }, cancellation) => {
		if (!host.runTool) throw new Error(`No browser tool handler is available for ${name}`);
		return host.runTool(name, args, stdin, cancellation);
	});
	return {
		dispose() {
			files.dispose();
			tools.dispose();
		},
	};
}
