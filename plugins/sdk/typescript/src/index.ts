export type JSONValue = null | boolean | number | string | JSONValue[] | { [key: string]: JSONValue };
export type Severity = 'error' | 'warning' | 'info';
export const RULE_CATEGORIES = ['correctness', 'suspicious', 'security', 'conventions'] as const;
export type RuleCategory = (typeof RULE_CATEGORIES)[number];
export function isRuleCategory(value: unknown): value is RuleCategory {
	return RULE_CATEGORIES.some((category) => category === value);
}
/** Safe fixes preserve intended behavior, unsafe fixes can change it, and suggested fixes need a decision. */
export type Applicability = 'safe' | 'unsafe' | 'suggested';
/** One-based LF line and Unicode character column in the request source. */
export interface Location {
	row: number;
	column: number;
}
/** One atomic fix alternative. All replacements belong to the request's captured source. */
export interface Fix {
	applicability?: Applicability;
	description?: string;
	/** Nonempty list of edits applied together. Range ends are exclusive, and an empty range inserts text. */
	replacements: {
		row: number;
		column: number;
		end_row: number;
		end_column: number;
		text: string;
	}[];
}
/** An active finding after native suppression. actionlint applies its own ignore rules afterwards. */
export interface Diagnostic {
	filename: string;
	code: string;
	message: string;
	/** Omit to use the rule's policy severity. */
	severity?: Severity;
	/** Script checks use snippet-local coordinates. */
	location: Location;
	/** Exclusive range end. Omit for a point finding. */
	end_location?: Location;
	/** Declare provider.fixes=true before returning these. */
	fixes?: Fix[];
}
export interface ScriptRequest {
	/** The plugin interprets native directives. */
	source: string;
	filename: string;
	/** Repository tools.<rule-name>.config settings. Validate before use. */
	config?: JSONValue;
	/** Scoped names for each possible runner OS. The request carries no environment values or secrets. */
	environment?: ScriptEnvironment[];
}

/**
 * Environment-name behavior for one possible execution OS. Windows merges names
 * without case, and Bash looks up the exported spelling exactly. A runner or an
 * earlier command can still supply missing names through GITHUB_ENV.
 */
export interface ScriptEnvironment {
	operating_system: 'linux' | 'macos' | 'windows';
	/** Comparison used when inner declarations override outer declarations. */
	merge_comparison: 'sensitive' | 'insensitive';
	/** Unknown means actionlint has not modeled the shell. */
	lookup_comparison: 'sensitive' | 'insensitive' | 'unknown';
	/** Declared exported keys, in first-declaration order. */
	names: {
		name: string;
		/** Scope that first introduced this key. */
		scope: 'workflow' | 'job' | 'caller' | 'step' | 'runtime';
		/** False when an unresolved earlier Windows declaration could have introduced another spelling. */
		case_known: boolean;
	}[];
	/** Some declaration keys could not be resolved. A key missing from names may still exist at runtime. */
	unknown_names: boolean;
}
/** The SDK runners use this wire response. Normal callbacks return diagnostics directly. */
export interface ScriptOutput {
	ok: boolean;
	diagnostics: Diagnostic[];
	errors: string[];
}
/** Return findings, or throw when analysis cannot complete. */
export type ScriptCheck = (request: ScriptRequest) => Diagnostic[] | Promise<Diagnostic[]>;
export type DocumentKind = 'workflow' | 'action';
export type DocumentRequirement = 'source' | 'dependencies';
/** Host-supplied identity for an existing declaration. Reuse the identity as given. */
export interface DocumentReference {
	document: string;
	/** Positions apply to this captured revision only. */
	revision: string;
	node: string;
	address: string;
}
/** One-based YAML source position. */
export interface DocumentPosition {
	line: number;
	/** Unicode character column. */
	column: number;
}
/** A statically known uses declaration. The declaration carries no fetched remote content. */
export interface DocumentDependency {
	reference: DocumentReference;
	uses: string;
	/** "action" or "workflow". */
	kind: string;
	/** Start of the complete YAML scalar, including opening quotes when present. */
	start: DocumentPosition;
	/** Exclusive end of that scalar, including closing quotes when present. */
	end: DocumentPosition;
}
/** Analysis should use the captured source and skip rereading it. */
export interface DocumentRequest {
	/** Exact captured YAML, including comments. Interpret native directives. The host handles actionlint ignores. */
	source: string;
	filename: string;
	kind: DocumentKind;
	/** Validate before use. */
	config?: JSONValue;
	/** Analysis-root path, supplied when directory/workspace resources were requested. */
	directory?: string;
	/** Supplied only when requiredInputs includes "dependencies". */
	dependencies?: DocumentDependency[];
}
/** A finding whose positions refer to the complete captured document. */
export type DocumentDiagnostic = Diagnostic;
/** Return findings, or throw when analysis cannot complete. */
export type DocumentCheck = (request: DocumentRequest) => DocumentDiagnostic[] | Promise<DocumentDiagnostic[]>;

export interface RuleDescriptor {
	/** Stable rule/config key used by host ignores, such as "yamllint". Native check identifiers belong in Diagnostic.code. */
	name: string;
	category: RuleCategory;
	subcategory: string;
	/** "nursery" marks an experimental rule. */
	maturity: 'stable' | 'nursery';
	description: string;
	provider: string;
	/** Include the rule in the recommended preset. Defaults to false. */
	recommended?: boolean;
	explanation?: string;
	help?: string;
	documentation_url?: string;
}
export interface InvocationOption {
	exit_after?: 'option' | 'long-options';
	/** Single short-option character. Bundled short options are parsed individually. */
	short?: string;
	/** Long option name without the leading two dashes. */
	long?: string;
	/** Consume the next argument as this option's value. */
	argument?: boolean;
	/** Omit for unrestricted values. */
	choices?: readonly string[];
	/** Setting family, such as set or shopt, reported to script preparation. */
	group?: string;
	/** Fixed setting name for a flag that takes no argument. */
	effect?: string;
	input?: 'command' | 'stdin';
	warning?: string;
}
/** Describe an interpreter or delegate a wrapper's arguments to another plugin's definition. */
export interface InvocationDefinition {
	/** Unique across plugins. */
	id: string;
	executables?: readonly string[];
	ignore_executable_case?: boolean;
	/** Exact arguments immediately after the executable, removed before validation. */
	prefix?: readonly string[];
	/** Borrow another definition's language and options, then add or replace options. */
	extends?: string;
	/** Validate forwarded arguments with this definition instead. */
	delegate?: string;
	language?: string;
	options?: readonly InvocationOption[];
	/** Accept +options to disable settings. */
	toggle_options?: boolean;
	/** Reject long options after a short option. */
	long_options_first?: boolean;
	/** Report unmodeled options as unknown. */
	unknown_options?: boolean;
	/** The runner supplies these arguments for a bare explicit interpreter name. */
	default_arguments?: readonly string[];
	/** Arguments supplied for the runner's implicit interpreter selection. */
	implicit_arguments?: readonly string[];
}
export interface ProviderIdentity {
	/** Interpreter grammars and wrapper routes registered when this plugin is loaded. */
	invocationDefinitions?: readonly InvocationDefinition[];
	/** Stable across releases, for example "github.com/owner/actionlint-plugin-example". */
	id: string;
	/** Plugin package version. It must match publication metadata. */
	version: string;
	name: string;
	description: string;
	license: string;
	/** Canonical HTTPS publishing repository URL. Registry publisher proof must match it. */
	repository: string;
	rule: RuleDescriptor;
	/** Virtual script basename, such as "actionlint.py", when a tool requires an extension. Ignored for documents. */
	filename?: string;
	fixes?: boolean;
	/** JSON Schema for user settings. Bundle all references. */
	configSchema?: Record<string, JSONValue>;
	/** Message format using {code}, {message}, {severity}, {row} and {column}. */
	messageTemplate?: string;
	/** Map wrapped-tool severity strings to actionlint severities. */
	severities?: Record<string, Severity>;
	/**
	 * Host environment variable names copied verbatim into the provider process.
	 * Defaults to none. Declare PATH when a callback launches another installed
	 * command, and TEMP/TMP when it needs the host's temporary directory. Windows
	 * tools may also need SystemRoot, COMSPEC or PATHEXT.
	 */
	env_passthrough?: string[];
	/**
	 * Document providers receive the analysis-root directory for either resource.
	 * Directory access requires captured source to match disk. This declaration
	 * does not sandbox a native process.
	 */
	resources?: ('workspace' | 'directory')[];
}
export interface CallbackBundleOptions {
	/** Defaults to neutral. Use node for Node-only dependencies or browser for browser conditions. */
	platform?: 'neutral' | 'node' | 'browser';
}

/** Create with defineScriptProvider. */
export interface ScriptProvider extends ProviderIdentity {
	/** defineScriptProvider supplies this discriminator. */
	kind: 'script';
	/** Defaults to Node. */
	runtime?: 'node' | 'bun' | 'deno';
	/** Interpreter flags before the generated .mjs filename. Deno receives `run` automatically. */
	runtimeArgs?: string[];
	bundle?: CallbackBundleOptions;
	/** Canonical shell basenames, for example ['pwsh', 'powershell']. */
	shells: string[];
	check: ScriptCheck;
}
export interface ProcessOptions extends ProviderIdentity {
	/** defineProcessProvider supplies this discriminator. */
	kind: 'process';
	/** Bare executable name resolved on the host's PATH. */
	entry: string;
	/** The runner passes the tokens without a shell. Defaults to []. Keep untrusted source out of command text. */
	args?: string[];
	env?: Record<string, string>;
	/** Variables to remove, even when a passthrough or fixed environment entry supplies them. */
	env_unset?: string[];
	/** Accepted scanner statuses, including finding statuses such as [0, 1]. Defaults to [0]. */
	exit_codes?: number[];
	/** Select serial execution for a tool that cannot safely handle concurrent requests. */
	concurrency?: 'pool' | 'serial';
	versionProbe?: {
		/** Literal probe arguments without placeholder substitution. */
		args: string[];
		/** RE2 pattern with exactly one capture group containing the tool version. */
		pattern: string;
		minimum?: string;
	};
	/** "request" sends the JSON request and is required for json-stdin settings. */
	stdin: 'request' | 'source';
	config: {
		/** "json-stdin" puts settings in the JSON request's config field. */
		render: 'none' | 'json-stdin' | 'json-file' | 'toml-file' | 'toml-pairs';
		/** Dotted keys rejected after transforming settings, such as ['output', 'fix']. */
		forbidden?: string[];
		/** jqConfigFile takes precedence. */
		transform?: {
			language: 'jq';
			inline: string;
		} | JQFile;
	};
	/** Stderr is reserved for execution errors. */
	output: {
		format: 'script-diagnostics@1' | 'json' | 'text' | 'sarif@2.1.0';
		/** Omit for one-based LF/Unicode scalar columns with exclusive ends. */
		coordinates?: {
			/** Whether the tool treats only LF or also lone CR as line separators. */
			rows: 'lf' | 'source';
			/** PowerShell uses utf16. */
			columns: 'chars' | 'bytes' | 'utf16';
			end: 'exclusive' | 'inclusive';
		};
		/** jqOutput loads a file at pack time. */
		transform?: {
			language: 'jq';
			inline: string;
		} | JQFile;
	};
	/** Relative to the provider definition. Pack embeds its contents. */
	jqConfigFile?: string;
	/** Relative to the provider definition. It takes precedence over output.transform. */
	jqOutputFile?: string;
}
export interface ProcessProvider extends ProcessOptions {
	/** Use shell preparation to infer the interpreter, reproduce its options and map inserted comments. */
	preparation?: 'shell';
	/** JSON pointers to path strings or path arrays whose configuration variables actionlint expands. */
	configurationPaths?: string[];
	/** Convert validated config into {options, source_prefix?, language?, read_access?} before execution. */
	configurationTransform?: { language: 'jq'; inline: string } | JQFile;
	/** Choose generated comments, startup commands, options and argument variables from interpreter and environment facts. */
	preparationTransform?: { language: 'jq'; inline: string } | JQFile;
	/** Use DocumentProcessProvider for complete YAML documents. */
	role?: 'script';
	shells: string[];
}
/** Create with defineDocumentProvider. */
export interface DocumentProvider extends ProviderIdentity {
	/** defineDocumentProvider supplies this discriminator. */
	kind: 'document';
	/** Defaults to Node. */
	runtime?: 'node' | 'bun' | 'deno';
	/** Interpreter flags before the generated .mjs filename. Deno receives `run` automatically. */
	runtimeArgs?: string[];
	bundle?: CallbackBundleOptions;
	/** Defaults to both workflow and action. */
	documentKinds?: DocumentKind[];
	/** Defaults to ['source']. */
	requiredInputs?: DocumentRequirement[];
	check: DocumentCheck;
}
export interface DocumentProcessProvider extends ProcessOptions {
	role: 'document';
	/** Defaults to both workflow and action. */
	documentKinds?: DocumentKind[];
	/** Defaults to ['source']. */
	requiredInputs?: DocumentRequirement[];
}
export type PluginProvider = ScriptProvider | ProcessProvider | DocumentProvider | DocumentProcessProvider;
export interface JQFile {
	/** Construct this marker with jqOutput. */
	readonly __jqFile: string;
}

/**
 * Define a callback for host-prepared shell snippets.
 * Default-export the provider from actionlint.plugin.ts, then use actionlint-plugin pack.
 */
export function defineScriptProvider<const T extends Omit<ScriptProvider, 'kind'>>(
	provider: T & Omit<ScriptProvider, 'kind'>,
): T & ScriptProvider {
	return { ...provider, kind: 'script' };
}
/**
 * Describe an installed executable that reads complete workflow/action documents.
 * Use role document; request dependencies only when the tool needs captured uses facts.
 * @example
 * ```ts
 * import { defineProcessProvider } from '@kjanat-actionlint/plugin-sdk';
 *
 * export default defineProcessProvider({
 *   id: 'example/workflow-checker', version: '0.1.0', name: 'Workflow checker',
 *   description: 'Wrap a tool that accepts complete workflow YAML.', license: 'MIT',
 *   repository: 'https://github.com/example/actionlint-plugin-workflow-checker',
 *   role: 'document', documentKinds: ['workflow'], requiredInputs: ['source'],
 *   entry: 'workflow-checker', args: ['--sarif'], stdin: 'request',
 *   config: { render: 'none' }, output: { format: 'sarif@2.1.0' },
 *   env_passthrough: ['PATH'],
 *   rule: { name: 'workflow-checker', category: 'correctness', subcategory: 'workflow', maturity: 'stable',
 *     description: 'Check workflows', provider: 'example/workflow-checker' },
 * });
 * ```
 */
export function defineProcessProvider<const T extends Omit<DocumentProcessProvider, 'kind'>>(
	provider: T & Omit<DocumentProcessProvider, 'kind'>,
): T & DocumentProcessProvider;
/**
 * Describe an installed executable for prepared script snippets; actionlint selects supported shells.
 * Arguments are separate tokens. actionlint checks the jq transforms and schema.
 * @example
 * ```ts
 * import { defineProcessProvider, jqOutput } from '@kjanat-actionlint/plugin-sdk';
 *
 * export default defineProcessProvider({
 *   id: 'example/checker', version: '0.1.0', name: 'Checker',
 *   description: 'Wrap the installed checker command.', license: 'MIT',
 *   repository: 'https://github.com/example/actionlint-plugin-checker',
 *   shells: ['bash'], entry: 'checker', args: ['--json'], stdin: 'source',
 *   config: { render: 'none' },
 *   output: { format: 'json', transform: jqOutput('./output.jq') },
 *   env_passthrough: ['PATH'],
 *   rule: { name: 'checker', category: 'correctness', subcategory: 'shell', maturity: 'stable',
 *     description: 'Run checker', provider: 'example/checker' },
 * });
 * ```
 */
export function defineProcessProvider<const T extends Omit<ProcessProvider, 'kind'>>(
	provider: T & Omit<ProcessProvider, 'kind'>,
): T & ProcessProvider;
export function defineProcessProvider(
	provider: Omit<ProcessProvider, 'kind'> | Omit<DocumentProcessProvider, 'kind'>,
): ProcessProvider | DocumentProcessProvider {
	return { ...provider, kind: 'process' };
}
/**
 * Define a callback for complete workflow/action YAML documents. Source is always present;
 * request dependency facts or directory resources only when the callback uses them.
 * @example
 * ```ts
 * import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
 *
 * export default defineDocumentProvider({
 *   id: 'example/document-note', version: '0.1.0', name: 'Document note',
 *   description: 'Example whole-document check.', license: 'MIT',
 *   repository: 'https://github.com/example/actionlint-plugin-document-note',
 *   documentKinds: ['workflow'], requiredInputs: ['source'],
 *   rule: { name: 'document-note', category: 'conventions', subcategory: 'workflow', maturity: 'nursery',
 *     description: 'Example workflow check', provider: 'example/document-note' },
 *   check: ({ source, filename }) => source.startsWith('TODO') ? [{
 *     filename, code: 'document-note', message: 'Resolve the TODO in this workflow',
 *     location: { row: 1, column: 1 },
 *   }] : [],
 * });
 * ```
 */
export function defineDocumentProvider<const T extends Omit<DocumentProvider, 'kind'>>(
	provider: T & Omit<DocumentProvider, 'kind'>,
): T & DocumentProvider & { documentKinds: DocumentKind[]; requiredInputs: DocumentRequirement[] } {
	return {
		...provider,
		kind: 'document',
		documentKinds: provider.documentKinds ?? ['workflow', 'action'],
		requiredInputs: provider.requiredInputs ?? ['source'],
	};
}
/**
 * Reference a jq output transform relative to the provider definition file.
 * The pack command embeds the file contents in the plugin package.
 * @param path Source file to embed, for example './diagnostics.jq'.
 */
export function jqOutput(path: string): JQFile {
	return { __jqFile: path };
}

/** Check whether parsed data contains only finite JSON-compatible values before using it as settings. */
export function isJSONValue(value: unknown): value is JSONValue {
	if (value === null || typeof value === 'string' || typeof value === 'boolean') return true;
	if (typeof value === 'number') return Number.isFinite(value);
	if (Array.isArray(value)) return value.every(isJSONValue);
	if (typeof value !== 'object') return false;
	return Object.values(value).every(isJSONValue);
}

/**
 * Validate a serialized script request and run a callback without accessing stdin/stdout.
 * @example
 * ```ts
 * import { executeScriptCheck } from '@kjanat-actionlint/plugin-sdk';
 * const result = await executeScriptCheck(
 *   ({ source, filename }) => source.startsWith('eval') ? [{
 *     filename, code: 'no-eval', message: 'Avoid eval', location: { row: 1, column: 1 },
 *   }] : [],
 *   JSON.stringify({ source: 'eval "$x"', filename: 'script.sh' }),
 * );
 * if (!result.ok) throw new Error(result.errors.join('; '));
 * ```
 * @param check Callback under test.
 * @param input Serialized ScriptRequest JSON.
 * @param outputLimit Maximum serialized response bytes; defaults to 8 MiB.
 * @param inputLimit Maximum serialized request bytes; defaults to 8 MiB.
 */
export async function executeScriptCheck(
	check: ScriptCheck,
	input: string,
	outputLimit = 8 * 1024 * 1024,
	inputLimit = 8 * 1024 * 1024,
): Promise<ScriptOutput> {
	if (new TextEncoder().encode(input).byteLength > inputLimit) {
		return { ok: false, diagnostics: [], errors: [`request exceeds ${inputLimit} byte limit`] };
	}
	let request: ScriptRequest;
	try {
		const value: unknown = JSON.parse(input);
		if (
			!isRecord(value) || typeof value.source !== 'string' || typeof value.filename !== 'string'
			|| ('config' in value && !isJSONValue(value.config))
		) throw new Error('expected {source, filename, config?}');
		request = { source: value.source, filename: value.filename };
		if ('config' in value && isJSONValue(value.config)) request.config = value.config;
		if ('environment' in value) {
			if (!Array.isArray(value.environment) || !value.environment.every(isScriptEnvironment)) {
				throw new Error('invalid script environment');
			}
			request.environment = value.environment;
		}
	} catch (error) {
		return { ok: false, diagnostics: [], errors: [messageOf(error)] };
	}
	try {
		const diagnostics = await check(request);
		if (!Array.isArray(diagnostics) || diagnostics.length > 10000 || !diagnostics.every(isDiagnostic)) {
			throw new Error('checker returned invalid diagnostics');
		}
		const output = { ok: true, diagnostics, errors: [] };
		if (new TextEncoder().encode(JSON.stringify(output)).byteLength > outputLimit) {
			return { ok: false, diagnostics: [], errors: [`response exceeds ${outputLimit} byte limit`] };
		}
		return output;
	} catch (error) {
		return { ok: false, diagnostics: [], errors: [messageOf(error)] };
	}
}

function isScriptEnvironment(value: unknown): value is ScriptEnvironment {
	return isRecord(value)
		&& ['linux', 'macos', 'windows'].some(os => os === value.operating_system)
		&& ['sensitive', 'insensitive'].some(comparison => comparison === value.merge_comparison)
		&& ['sensitive', 'insensitive', 'unknown'].some(comparison => comparison === value.lookup_comparison)
		&& typeof value.unknown_names === 'boolean'
		&& Array.isArray(value.names)
		&& value.names.every((variable: unknown) =>
			isRecord(variable) && typeof variable.name === 'string'
			&& typeof variable.case_known === 'boolean'
			&& ['workflow', 'job', 'caller', 'step', 'runtime'].some(scope => scope === variable.scope)
		);
}

/**
 * Validate a serialized document request and run a callback without process I/O.
 * Only documented JSON fields reach the callback; malformed facts and callback failures produce
 * a protocol failure.
 * @param check Callback under test.
 * @param input Serialized DocumentRequest JSON, including its workflow/action kind.
 * @param outputLimit Maximum serialized response bytes; defaults to 8 MiB.
 * @param inputLimit Maximum serialized request bytes; defaults to 8 MiB.
 */
export async function executeDocumentCheck(
	check: DocumentCheck,
	input: string,
	outputLimit = 8 * 1024 * 1024,
	inputLimit = 8 * 1024 * 1024,
): Promise<ScriptOutput> {
	if (new TextEncoder().encode(input).byteLength > inputLimit) {
		return { ok: false, diagnostics: [], errors: [`request exceeds ${inputLimit} byte limit`] };
	}
	let request: DocumentRequest;
	try {
		const value: unknown = JSON.parse(input);
		request = parseDocumentRequest(value);
	} catch (error) {
		return { ok: false, diagnostics: [], errors: [messageOf(error)] };
	}
	try {
		const diagnostics = await check(request);
		if (!Array.isArray(diagnostics) || diagnostics.length > 10000 || !diagnostics.every(isDiagnostic)) {
			throw new Error('checker returned invalid diagnostics');
		}
		const output = { ok: true, diagnostics, errors: [] };
		if (new TextEncoder().encode(JSON.stringify(output)).byteLength > outputLimit) {
			return { ok: false, diagnostics: [], errors: [`response exceeds ${outputLimit} byte limit`] };
		}
		return output;
	} catch (error) {
		return { ok: false, diagnostics: [], errors: [messageOf(error)] };
	}
}

/**
 * Convert a one-based UTF-16 column from a JavaScript parser/tool to a Unicode character column.
 * Pass the relevant source line without its newline; use the result in diagnostic locations.
 * Values outside the line are clamped to its boundaries.
 * @example unicodeColumn('a😀b', 4) // 3: b follows one emoji, which counts as one character.
 */
export function unicodeColumn(sourceLine: string, utf16Column: number): number {
	const index = Math.max(0, Math.min(sourceLine.length, utf16Column - 1));
	return [...sourceLine.slice(0, index)].length + 1;
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function isDocumentRequest(value: unknown): value is DocumentRequest {
	return isRecord(value) && typeof value.source === 'string' && typeof value.filename === 'string'
		&& (value.kind === 'workflow' || value.kind === 'action')
		&& (!('config' in value) || isJSONValue(value.config))
		&& (!('directory' in value) || typeof value.directory === 'string')
		&& (!('dependencies' in value)
			|| (Array.isArray(value.dependencies) && value.dependencies.every(isDocumentDependency)));
}
function parseDocumentRequest(value: unknown): DocumentRequest {
	if (!isDocumentRequest(value)) {
		throw new Error('expected document request {source, filename, kind, config?, directory?, dependencies?}');
	}
	const request: DocumentRequest = { source: value.source, filename: value.filename, kind: value.kind };
	if ('config' in value) request.config = value.config;
	if (value.directory !== undefined) request.directory = value.directory;
	if (value.dependencies !== undefined) {
		request.dependencies = value.dependencies.map((dependency) => ({
			reference: { ...dependency.reference },
			uses: dependency.uses,
			kind: dependency.kind,
			start: { ...dependency.start },
			end: { ...dependency.end },
		}));
	}
	return request;
}
function isDocumentDependency(value: unknown): value is DocumentDependency {
	if (
		!isRecord(value) || !isRecord(value.reference) || !isDocumentPosition(value.start) || !isDocumentPosition(value.end)
	) {
		return false;
	}
	return typeof value.reference.document === 'string' && typeof value.reference.revision === 'string'
		&& typeof value.reference.node === 'string' && typeof value.reference.address === 'string'
		&& typeof value.uses === 'string' && typeof value.kind === 'string'
		&& (value.end.line > value.start.line
			|| (value.end.line === value.start.line && value.end.column >= value.start.column));
}
function isDocumentPosition(value: unknown): value is DocumentPosition {
	return isRecord(value) && isPositiveInteger(value.line) && isPositiveInteger(value.column);
}
function isLocation(value: unknown): value is Location {
	return isRecord(value) && Number.isInteger(value.row) && Number(value.row) >= 1 && Number.isInteger(value.column)
		&& Number(value.column) >= 1;
}
/**
 * Validate a decoded diagnostic before exposing it through a typed API.
 *
 * @example
 * ```ts
 * const value: unknown = JSON.parse(serializedFinding);
 * if (!isDiagnostic(value)) throw new Error('invalid scanner diagnostic');
 * console.log(value.code, value.location.row);
 * ```
 */
export function isDiagnostic(value: unknown): value is Diagnostic {
	return isRecord(value) && typeof value.filename === 'string' && typeof value.code === 'string'
		&& typeof value.message === 'string' && isLocation(value.location)
		&& (value.severity === undefined || value.severity === 'error' || value.severity === 'warning'
			|| value.severity === 'info')
		&& (value.end_location === undefined || isLocation(value.end_location))
		&& (value.fixes === undefined || (Array.isArray(value.fixes) && value.fixes.every(isFix)));
}
function isFix(value: unknown): value is Fix {
	if (!isRecord(value) || (value.description !== undefined && typeof value.description !== 'string')) return false;
	if (
		value.applicability !== undefined && value.applicability !== 'safe'
		&& value.applicability !== 'unsafe' && value.applicability !== 'suggested'
	) return false;
	if (!Array.isArray(value.replacements) || value.replacements.length === 0) return false;
	return value.replacements.every((replacement: unknown) => {
		if (!isRecord(replacement)) return false;
		const { row, column, end_row: endRow, end_column: endColumn, text } = replacement;
		if (
			!isPositiveInteger(row) || !isPositiveInteger(column) || !isPositiveInteger(endRow)
			|| !isPositiveInteger(endColumn) || typeof text !== 'string'
		) return false;
		return endRow > row || (endRow === row && endColumn >= column);
	});
}
function isPositiveInteger(value: unknown): value is number {
	return Number.isInteger(value) && Number(value) >= 1;
}
function messageOf(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
