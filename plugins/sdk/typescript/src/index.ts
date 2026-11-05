/** Values that can cross the JSON process protocol; functions and runtime handles cannot. */
export type JSONValue = null | boolean | number | string | JSONValue[] | { [key: string]: JSONValue };
/** A finding's severity before the repository's rule-level policy is applied. */
export type Severity = 'error' | 'warning' | 'info';
/** Semantic concern categories accepted in rule metadata. */
export const RULE_CATEGORIES = ['correctness', 'suspicious', 'security', 'conventions'] as const;
/** Semantic concern assigned to a rule descriptor. */
export type RuleCategory = (typeof RULE_CATEGORIES)[number];
/** Check untrusted metadata against the SDK's supported rule categories. */
export function isRuleCategory(value: unknown): value is RuleCategory {
	return RULE_CATEGORIES.some((category) => category === value);
}
/** Review required for one fix: safe preserves intended behavior; unsafe can change it; suggested needs a decision. */
export type Applicability = 'safe' | 'unsafe' | 'suggested';
/** One-based position in the request source, counting Unicode characters rather than UTF-16 units. */
export interface Location {
	/** LF line number; the first line is 1. */
	row: number;
	/** Unicode character column; an emoji counts as one character. */
	column: number;
}
/**
 * One atomic fix alternative. All replacements belong to the request's captured source.
 * Return several fixes to offer alternatives; omit fixes when this finding has no correction.
 *
 * @example
 * ```ts
 * const fix: Fix = {
 *   description: 'Remove trailing spaces',
 *   applicability: 'safe',
 *   replacements: [{ row: 1, column: 8, end_row: 1, end_column: 10, text: '' }],
 * }
 * ```
 */
export interface Fix {
	/** Applicability of this concrete alternative. Omit to require review; safe allows automatic application. */
	applicability?: Applicability;
	/** Short editor action label, such as "Replace alias with full command". */
	description?: string;
	/** Nonempty list of edits applied together; use an empty range for an insertion. */
	replacements: {
		/** First line of the replaced range, starting at 1. */
		row: number;
		/** First Unicode character column of the replaced range, starting at 1. */
		column: number;
		/** Line containing the exclusive range end; may follow the start line. */
		end_row: number;
		/** Exclusive Unicode character column; equal start/end positions insert text. */
		end_column: number;
		/** Replacement source text; an empty string deletes the range. */
		text: string;
	}[];
}
/**
 * An active finding after the plugin's native suppression processing.
 * The host maps snippet positions back to YAML, then applies actionlint ignores.
 */
export interface Diagnostic {
	/** Copy request.filename so the host can bind the finding to the captured source. */
	filename: string;
	/** Stable upstream or plugin diagnostic code, such as "SC2086". */
	code: string;
	/** Human-readable explanation and correction; exclude terminal colour escapes. */
	message: string;
	/** Tool-reported severity; omit when the host should use its rule policy. */
	severity?: Severity;
	/** First affected position in request.source; snippet checks use snippet-local coordinates. */
	location: Location;
	/** Exclusive range end; omit for a point finding when the tool supplies no extent. */
	end_location?: Location;
	/** Suggested source edits. Declare provider.fixes=true before returning these. */
	fixes?: Fix[];
}
/** Input to a script callback: one prepared run snippet, not a complete workflow. */
export interface ScriptRequest {
	/** Prepared script text; interpret native directives here in the plugin/linter. GitHub expressions may be masked. */
	source: string;
	/** Virtual snippet filename selected by the provider; use it in returned findings. */
	filename: string;
	/** Repository tools.<rule-name>.config settings, if supplied; validate before use. */
	config?: JSONValue;
}
/** Wire response used by the SDK runners; normal callbacks return diagnostics directly. */
export interface ScriptOutput {
	/** True when the check completed; false when request validation or execution failed. */
	ok: boolean;
	/** Findings from a completed callback. An empty array means no findings. */
	diagnostics: Diagnostic[];
	/** Execution/validation failures, distinct from findings about the user's source. */
	errors: string[];
}
/** Check one prepared snippet; return findings, or throw when analysis cannot complete. */
export type ScriptCheck = (request: ScriptRequest) => Diagnostic[] | Promise<Diagnostic[]>;
/** Document types a document provider can select: workflow YAML or action metadata YAML. */
export type DocumentKind = 'workflow' | 'action';
/** Input kinds to request; dependencies adds statically known uses references. */
export type DocumentRequirement = 'source' | 'dependencies';
/** Host-supplied identity for an existing declaration; preserve it instead of inventing one. */
export interface DocumentReference {
	/** Identity of the source document containing the declaration. */
	document: string;
	/** Captured document revision; positions apply to these bytes only. */
	revision: string;
	/** Stable declaration identity within the host's source model. */
	node: string;
	/** Declaration address within that document revision; separate from node identity. */
	address: string;
}
/** One-based YAML source position supplied with dependency facts. */
export interface DocumentPosition {
	/** Source line, starting at 1. */
	line: number;
	/** Unicode character column, starting at 1. */
	column: number;
}
/** A statically known action or reusable-workflow uses declaration, not fetched remote content. */
export interface DocumentDependency {
	/** Host source identity for this declaration and captured revision. */
	reference: DocumentReference;
	/** Declared uses value, for example "actions/checkout@v4". */
	uses: string;
	/** Host dependency kind: "action" or "workflow". */
	kind: string;
	/** Start of the complete YAML scalar, including opening quotes when present. */
	start: DocumentPosition;
	/** Exclusive end of that scalar, including closing quotes when present. */
	end: DocumentPosition;
}
/** Input to a whole-document callback; analysis should use captured source rather than rereading it. */
export interface DocumentRequest {
	/** Exact captured YAML, including comments and original spelling. Interpret native directives; the host handles actionlint ignores. */
	source: string;
	/** Captured document filename; copy it into each returned diagnostic. */
	filename: string;
	/** Whether source is a workflow or action metadata document. */
	kind: DocumentKind;
	/** lint.rules.plugins.<rule-name>.options settings, if supplied; validate before use. */
	config?: JSONValue;
	/** Analysis-root path, supplied when directory/workspace resources were requested. */
	directory?: string;
	/** Captured uses declarations; supplied only when requiredInputs includes "dependencies". */
	dependencies?: DocumentDependency[];
}
/** A finding whose positions refer to the complete captured document rather than a run snippet. */
export type DocumentDiagnostic = Diagnostic;
/** Check a complete document; return findings, or throw when analysis cannot complete. */
export type DocumentCheck = (request: DocumentRequest) => DocumentDiagnostic[] | Promise<DocumentDiagnostic[]>;

/** Rule identity shown in diagnostics and used to select this provider's options. */
export interface RuleDescriptor {
	/** Stable rule/config key used by host ignores, such as "yamllint". Native check identifiers belong in Diagnostic.code. */
	name: string;
	/** Catalogue concern: correctness, suspicious, security, or conventions. */
	category: RuleCategory;
	/** More specific catalogue grouping, for example "yaml" or "powershell". */
	subcategory: string;
	/** "nursery" marks an experimental rule; use "stable" after its behavior is established. */
	maturity: 'stable' | 'nursery';
	/** Short explanation of what the rule checks, shown in the rule catalogue. */
	description: string;
	/** Identity of the implementation providing this rule; normally the provider id. */
	provider: string;
	/** Include the rule in the host's recommended preset. Defaults to false. */
	recommended?: boolean;
	/** Longer rule rationale and examples shown by rule-inspection commands. */
	explanation?: string;
	/** Short remediation guidance shown with this rule. */
	help?: string;
	/** Public documentation URL for this rule. */
	documentation_url?: string;
}
/** Metadata and host requirements shared by callback providers and existing executable wrappers. */
export interface ProviderIdentity {
	/** Stable plugin identity across releases, for example "github.com/owner/actionlint-plugin-example". */
	id: string;
	/** Plugin package version, not the wrapped scanner's version; must match publication metadata. */
	version: string;
	/** Human-readable plugin name for discovery and registry display. */
	name: string;
	/** Brief plugin purpose, including which language or documents it checks. */
	description: string;
	/** Plugin's license identifier, for example "MIT"; separate from the external tool's license. */
	license: string;
	/** Canonical HTTPS publishing repository URL; registry publisher proof must match it. */
	repository: string;
	/** Diagnostic rule identity and catalogue details for this provider. */
	rule: RuleDescriptor;
	/** Virtual script basename, such as "actionlint.py", when a tool requires an extension. Ignored for documents. */
	filename?: string;
	/** Set true when returning suggested fixes. Defaults to false; does not apply edits automatically. */
	fixes?: boolean;
	/** JSON Schema for user settings. References must be bundled; validation never fetches remote schemas. */
	configSchema?: Record<string, JSONValue>;
	/** Message format using {code}, {message}, {severity}, {row} and {column}. */
	messageTemplate?: string;
	/** Map wrapped-tool severity strings to actionlint severities. */
	severities?: Record<string, Severity>;
	/**
	 * Host environment variable names copied verbatim into the provider process.
	 * Defaults to none; absent variables are skipped. Declare PATH when a callback
	 * launches another installed command, and TEMP/TMP when it needs the host's
	 * temporary directory. Windows tools may also need SystemRoot, COMSPEC or PATHEXT.
	 * HOME/USERPROFILE enable user configuration discovery; include only what the
	 * implementation needs. This selects names, not values, and does not install tools.
	 *
	 * @example
	 * env_passthrough: ['PATH', 'SystemRoot', 'TEMP', 'TMP']
	 */
	env_passthrough?: string[];
	/**
	 * Extra host context needed by the checker. Document providers receive the
	 * analysis-root directory for either resource; omit for source-only checks.
	 * Directory access requires captured source to match disk. This declaration
	 * does not sandbox a native process or restrict its operating-system rights.
	 */
	resources?: ('workspace' | 'directory')[];
}
/**
 * Bundler resolution policy, independent of the interpreter running the artifact.
 *
 * @example
 * ```ts
 * bundle: { platform: 'node' } // Explicit CommonJS/Node dependency interop.
 * ```
 */
export interface CallbackBundleOptions {
	/** Defaults to neutral. Use node for Node-only dependencies or browser for browser conditions. */
	platform?: 'neutral' | 'node' | 'browser';
}

/** TypeScript callback for prepared scripts; create with defineScriptProvider. */
export interface ScriptProvider extends ProviderIdentity {
	/** Discriminator supplied by defineScriptProvider; do not repeat it in the author definition. */
	kind: 'script';
	/** Interpreter for the bundled JavaScript. Defaults to Node; Bun and Deno are opt-in. */
	runtime?: 'node' | 'bun' | 'deno';
	/** Interpreter flags before the generated .mjs filename; Deno receives `run` automatically. */
	runtimeArgs?: string[];
	/** Explicit dependency resolution policy. Omit for a neutral bundle. */
	bundle?: CallbackBundleOptions;
	/** Canonical shell basenames to select, for example ['pwsh', 'powershell']. */
	shells: string[];
	/** Analyze each selected snippet. pack bundles this callback with its declared interpreter and dependency policy. */
	check: ScriptCheck;
}
/** Describe an installed executable when its existing output can be decoded without a callback. */
export interface ProcessOptions extends ProviderIdentity {
	/** Discriminator supplied by defineProcessProvider. */
	kind: 'process';
	/** Bare executable name resolved on the host's PATH, for example "pwsh". */
	entry: string;
	/** Argument tokens, passed without a shell. Defaults to []; never interpolate untrusted source into command text. */
	args?: string[];
	/** Fixed environment entries for reproducible tool behavior, such as { NO_COLOR: '1' }. */
	env?: Record<string, string>;
	/** Variables removed even if supplied by a passthrough or fixed environment entry. */
	env_unset?: string[];
	/** Accepted scanner statuses, including finding statuses such as [0, 1]. Defaults to [0]. */
	exit_codes?: number[];
	/** Select serial execution for a tool that cannot safely handle concurrent requests. */
	concurrency?: 'pool' | 'serial';
	/** Optional installed-tool version probe, independent of manifest/plugin version. */
	versionProbe?: {
		/** Literal probe arguments; no source/config placeholders are substituted. */
		args: string[];
		/** RE2 pattern with exactly one capture group containing the tool version. */
		pattern: string;
		/** Minimum supported tool version, such as '1.25.0'. */
		minimum?: string;
	};
	/** "source" sends plain source; "request" sends the JSON request, required for json-stdin settings. */
	stdin: 'request' | 'source';
	/** How repository plugin settings reach the wrapped executable. */
	config: {
		/** "none" adds no settings; "json-stdin" includes them in the JSON request's config field. */
		render: 'none' | 'json-stdin' | 'json-file' | 'toml-file' | 'toml-pairs';
		/** Dotted keys rejected after transforming settings, such as ['output', 'fix']. */
		forbidden?: string[];
		/** jq program adapting user settings to the executable's request format; jqConfigFile takes precedence. */
		transform?: {
			/** Transform language; currently only jq is supported. */
			language: 'jq';
			/** jq source converting user settings to the process configuration payload. */
			inline: string;
		} | JQFile;
	};
	/** Select the executable's stdout decoder; stderr is reserved for execution errors. */
	output: {
		/** SDK envelope, arbitrary JSON transformed by jq, or standard SARIF 2.1.0 output. */
		format: 'script-diagnostics@1' | 'json' | 'text' | 'sarif@2.1.0';
		/** Wrapped-tool positions. Omit for one-based LF/Unicode scalar columns with exclusive ends. */
		coordinates?: {
			/** Whether the tool treats only LF or also lone CR as line separators. */
			rows: 'lf' | 'source';
			/** Units reported by the tool; PowerShell uses utf16. */
			columns: 'chars' | 'bytes' | 'utf16';
			/** Whether the last reported position is inside or after the affected range. */
			end: 'exclusive' | 'inclusive';
		};
		/** Required for arbitrary JSON: map records into the host diagnostics format. jqOutput loads a file at pack time. */
		transform?: {
			/** Transform language; the host runs jq without an external jq installation. */
			language: 'jq';
			/** jq source converting decoded tool output into a diagnostics array. */
			inline: string;
		} | JQFile;
	};
	/** jq settings-transform file, relative to the provider definition; pack embeds its contents. */
	jqConfigFile?: string;
	/** jq output-transform file, relative to the provider definition; takes precedence over output.transform. */
	jqOutputFile?: string;
}
/** Existing executable that checks prepared run snippets. */
export interface ProcessProvider extends ProcessOptions {
	/** Omit for a script wrapper; use DocumentProcessProvider for complete YAML documents. */
	role?: 'script';
	/** Canonical shell basenames to route to the executable, for example ['bash', 'sh']. */
	shells: string[];
}
/** TypeScript callback for complete YAML documents; create with defineDocumentProvider. */
export interface DocumentProvider extends ProviderIdentity {
	/** Discriminator supplied by defineDocumentProvider. */
	kind: 'document';
	/** Interpreter for the bundled JavaScript. Defaults to Node; Bun and Deno are opt-in. */
	runtime?: 'node' | 'bun' | 'deno';
	/** Interpreter flags before the generated .mjs filename; Deno receives `run` automatically. */
	runtimeArgs?: string[];
	/** Explicit dependency resolution policy. Omit for a neutral bundle. */
	bundle?: CallbackBundleOptions;
	/** Document types to analyze. Defaults to both workflow and action; restrict unsupported formats. */
	documentKinds?: DocumentKind[];
	/** Input kinds needed by check. Defaults to ['source']; add 'dependencies' for captured uses declarations. */
	requiredInputs?: DocumentRequirement[];
	/** Analyze captured YAML. pack embeds the callback and its declared runtime dependencies. */
	check: DocumentCheck;
}
/** Existing executable that consumes complete workflow/action documents instead of snippets. */
export interface DocumentProcessProvider extends ProcessOptions {
	/** Required to send whole documents rather than prepared script snippets. */
	role: 'document';
	/** Supported document types. Defaults to both workflow and action. */
	documentKinds?: DocumentKind[];
	/** Required inputs. Defaults to ['source']; dependencies are included only when explicitly requested. */
	requiredInputs?: DocumentRequirement[];
}
/** Supported author definitions; the pack CLI chooses transport and preparation from their discriminator. */
export type PluginProvider = ScriptProvider | ProcessProvider | DocumentProvider | DocumentProcessProvider;
/** Deferred jq source file produced by jqOutput; pack reads and embeds the program. */
export interface JQFile {
	/** Provider-relative source filename; construct this marker with jqOutput. */
	readonly __jqFile: string;
}

/**
 * Define a callback for host-prepared shell snippets. The callback is bundled as a native stdio
 * provider; bundle resolution defaults to neutral and runtime defaults to Node.
 * Default-export the provider from actionlint.plugin.ts, then use actionlint-plugin pack.
 * @example
 * ```ts
 * import { defineScriptProvider } from '@kjanat-actionlint/plugin-sdk';
 *
 * export default defineScriptProvider({
 *   id: 'example/no-tabs', version: '0.1.0', name: 'No tabs',
 *   description: 'Find tab characters in shell snippets.', license: 'MIT',
 *   repository: 'https://github.com/example/actionlint-plugin-no-tabs',
 *   shells: ['bash', 'sh'],
 *   rule: { name: 'no-tabs', category: 'conventions', subcategory: 'shell', maturity: 'stable',
 *     description: 'Reject tabs', provider: 'example/no-tabs' },
 *   check: ({ source, filename }) => source.startsWith('\\t') ? [{
 *     filename, code: 'no-tabs', message: 'Replace the tab with spaces',
 *     location: { row: 1, column: 1 },
 *   }] : [],
 * });
 * ```
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
 * Describe an installed executable for prepared script snippets; the host selects supported shells.
 * Arguments are separate tokens; jq transforms and schema are checked by the host.
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
 * pack embeds the file contents, so authors need neither a manual digest nor a runtime jq executable.
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
 * Input is JSON containing source and filename, not raw source text. UTF-8 request and response
 * sizes default to 8 MiB; malformed input, invalid findings, and thrown errors become failures.
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
 * @param input Serialized ScriptRequest JSON, not bare script source.
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
 * Validate a serialized document request and run a callback without process I/O.
 * Only documented JSON fields reach the callback; malformed facts and callback failures produce
 * a protocol failure. UTF-8 input/output size limits default to 8 MiB.
 * @example
 * ```ts
 * import { executeDocumentCheck } from '@kjanat-actionlint/plugin-sdk';
 * const result = await executeDocumentCheck(
 *   ({ source, filename }) => source.startsWith('uses: actions/checkout@master') ? [{
 *     filename, code: 'pin-action', message: 'Pin the action to a full commit SHA',
 *     location: { row: 1, column: 1 },
 *   }] : [],
 *   JSON.stringify({ source: 'uses: actions/checkout@master\\n', filename: '.github/workflows/ci.yml', kind: 'workflow' }),
 * );
 * if (!result.ok) throw new Error(result.errors.join('; '));
 * ```
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
 * @example unicodeColumn('a😀b', 4) // 3: b follows one emoji, not two characters.
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
 * Checks optional severities, ranges and complete fix replacement records.
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
