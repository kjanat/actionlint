import type { CancellationToken, Disposable, ProtocolConnection } from 'vscode-languageserver-protocol';
import { RequestType } from 'vscode-languageserver-protocol';

/** Files come from the editor host, including VS Code's workspace.fs API. */
export const ReadFileRequest = new RequestType<{ uri: string }, string | null, void>('actionlint/readFile');

export type FilesystemRequest = {
	operation: 'stat' | 'lstat' | 'readDirectory' | 'realpath';
	uri: string;
};
export type FileMetadata = {
	name: string;
	kind: 'file' | 'directory' | 'symlink' | 'other';
	size: number;
	modified: number;
};
export type FilesystemResponse =
	| { kind: 'metadata'; info: FileMetadata }
	| { kind: 'directory'; entries: FileMetadata[] }
	| { kind: 'path'; path: string }
	| null;
export type GitQueryParams = { uri: string; args: string[] };
export type GitQueryResult = { stdout: string; stderr: string; exitCode: number; path?: string };
export const FilesystemRequest = new RequestType<FilesystemRequest, FilesystemResponse, void>('actionlint/filesystem');
export const GitQueryRequest = new RequestType<GitQueryParams, GitQueryResult, void>('actionlint/gitQuery');

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
	filesystem?(request: FilesystemRequest, cancellation: CancellationToken): Promise<FilesystemResponse>;
	gitQuery?(request: GitQueryParams, cancellation: CancellationToken): Promise<GitQueryResult>;
	/** Read UTF-8 workspace text. Return null for a missing file. Do not fetch arbitrary URLs. */
	readFile(uri: string, cancellation: CancellationToken): Promise<string | null>;
	/** Run a tool explicitly named in initializationOptions.tools. Return its standard output. */
	runTool?(name: string, args: string[], stdin: string, cancellation: CancellationToken): Promise<string>;
}

/** Attach host operations to a protocol connection before opening documents. */
export function provideBrowserHost(connection: Pick<ProtocolConnection, 'onRequest'>, host: BrowserHost): Disposable {
	const filesystem = connection.onRequest(FilesystemRequest, (params, cancellation) => {
		if (!host.filesystem) throw new Error('This host does not provide filesystem discovery operations');
		return host.filesystem(params, cancellation);
	});
	const git = connection.onRequest(GitQueryRequest, (params, cancellation) => {
		if (!host.gitQuery) throw new Error('This host does not provide Git metadata queries');
		return host.gitQuery(params, cancellation);
	});
	const files = connection.onRequest(ReadFileRequest, ({ uri }, cancellation) => host.readFile(uri, cancellation));
	const tools = connection.onRequest(RunToolRequest, ({ name, args, stdin }, cancellation) => {
		if (!host.runTool) throw new Error(`No browser tool handler is available for ${name}`);
		return host.runTool(name, args, stdin, cancellation);
	});
	return {
		dispose() {
			filesystem.dispose();
			git.dispose();
			files.dispose();
			tools.dispose();
		},
	};
}
