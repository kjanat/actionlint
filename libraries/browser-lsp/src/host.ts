import type { CancellationToken, Disposable, ProtocolConnection } from 'vscode-languageserver-protocol';
import { RequestType } from 'vscode-languageserver-protocol';

export const ReadFileRequest = new RequestType<{ uri: string }, string | null, void>('actionlint/readFile');

export type FilesystemRequest = {
	operation: 'stat' | 'lstat' | 'readDirectory' | 'realpath';
	uri: string;
};
export type FileMetadata = {
	name: string;
	kind: 'file' | 'directory' | 'symlink' | 'other';
	size: number;
	/** Milliseconds since the Unix epoch. */
	modified: number;
};
/** Return null for a missing entry. A realpath result contains a workspace URI. */
export type FilesystemResponse =
	| { kind: 'metadata'; info: FileMetadata }
	| { kind: 'directory'; entries: FileMetadata[] }
	| { kind: 'path'; path: string }
	| null;
export type GitQueryParams = { uri: string; args: string[] };
export type GitQueryResult = {
	stdout: string;
	stderr: string;
	exitCode: number;
	/** A successful rev-parse --show-toplevel requires this field. */
	path?: string;
};
export const FilesystemRequest = new RequestType<FilesystemRequest, FilesystemResponse, void>('actionlint/filesystem');
export const GitQueryRequest = new RequestType<GitQueryParams, GitQueryResult | null, void>('actionlint/gitQuery');

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
	/** Return null for a work-tree lookup when Git confirms there is no enclosing repository. */
	gitQuery?(request: GitQueryParams, cancellation: CancellationToken): Promise<GitQueryResult | null>;
	/** Return null for a missing file. Do not fetch arbitrary URLs. */
	readFile(uri: string, cancellation: CancellationToken): Promise<string | null>;
	/** Run only tools explicitly named in initializationOptions.tools. */
	runTool?(name: string, args: string[], stdin: string, cancellation: CancellationToken): Promise<string>;
}

/** Call before opening documents. */
export function provideBrowserHost(connection: Pick<ProtocolConnection, 'onRequest'>, host: BrowserHost): Disposable {
	const filesystem = connection.onRequest(FilesystemRequest, (params, cancellation) => {
		if (!host.filesystem) throw new Error('This host does not provide filesystem discovery operations');
		return host.filesystem(params, cancellation);
	});
	const git = connection.onRequest(GitQueryRequest, (params, cancellation) => {
		if (host.gitQuery) return host.gitQuery(params, cancellation);
		if (params.args.length === 2 && params.args[0] === 'rev-parse' && params.args[1] === '--show-toplevel') return null;
		throw new Error('This host does not provide Git metadata queries');
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
