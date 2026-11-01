export type JSONValue = null | boolean | number | string | JSONValue[] | { [key: string]: JSONValue };
export type Severity = 'error' | 'warning' | 'info';
export interface Location {
	row: number;
	column: number;
}
export interface Fix {
	description?: string;
	replacements: { row: number; column: number; end_row: number; end_column: number; text: string }[];
}
export interface Diagnostic {
	filename: string;
	code: string;
	message: string;
	severity?: Severity;
	location: Location;
	end_location?: Location;
	fixes?: Fix[];
}
export interface ScriptRequest {
	source: string;
	filename: string;
	config?: JSONValue;
}
export interface ScriptOutput {
	ok: boolean;
	diagnostics: Diagnostic[];
	errors: string[];
}
export type ScriptCheck = (request: ScriptRequest) => Diagnostic[] | Promise<Diagnostic[]>;

export interface RuleDescriptor {
	name: string;
	category: string;
	subcategory: string;
	maturity: 'stable' | 'nursery';
	description: string;
	provider: string;
}
export interface ProviderBase {
	id: string;
	version: string;
	name: string;
	description: string;
	license: string;
	repository: string;
	rule: RuleDescriptor;
	shells: string[];
	filename?: string;
	fixes?: boolean;
	external?: string[];
}
export interface ScriptProvider extends ProviderBase {
	kind: 'script';
	check: ScriptCheck;
}
export interface ProcessProvider extends ProviderBase {
	kind: 'process';
	entry: string;
	args?: string[];
	stdin: 'request' | 'source';
	config: { render: 'none' | 'json-stdin'; transform?: { language: 'jq'; inline: string } };
	output: {
		format: 'script-diagnostics@1' | 'json' | 'sarif@2.1.0';
		transform?: { language: 'jq'; inline: string } | JQFile;
	};
	jqConfigFile?: string;
	jqOutputFile?: string;
	env_passthrough?: string[];
}
export type PluginProvider = ScriptProvider | ProcessProvider;
export interface JQFile {
	readonly __jqFile: string;
}

export function defineScriptProvider<const T extends Omit<ScriptProvider, 'kind'>>(
	provider: T,
): T & { kind: 'script' } {
	return { ...provider, kind: 'script' };
}
export function defineProcessProvider<const T extends Omit<ProcessProvider, 'kind'>>(
	provider: T,
): T & { kind: 'process' } {
	return { ...provider, kind: 'process' };
}
export function jqOutput(path: string): JQFile {
	return { __jqFile: path };
}

export function isJSONValue(value: unknown): value is JSONValue {
	if (value === null || typeof value === 'string' || typeof value === 'boolean') return true;
	if (typeof value === 'number') return Number.isFinite(value);
	if (Array.isArray(value)) return value.every(isJSONValue);
	if (typeof value !== 'object') return false;
	return Object.values(value).every(isJSONValue);
}

export async function serveScriptEngine(
	check: ScriptCheck,
	limits: { inputBytes?: number; outputBytes?: number } = {},
): Promise<void> {
	const inputLimit = limits.inputBytes ?? 8 * 1024 * 1024;
	const outputLimit = limits.outputBytes ?? 8 * 1024 * 1024;
	let input: string;
	try {
		input = await readStdin(inputLimit);
	} catch (error) {
		writeJSON({ ok: false, diagnostics: [], errors: [messageOf(error)] }, outputLimit);
		return;
	}
	const output = await executeScriptCheck(check, input, outputLimit, inputLimit);
	writeJSON(output, outputLimit);
}

export async function executeScriptCheck(
	check: ScriptCheck,
	input: string,
	outputLimit = 8 * 1024 * 1024,
	inputLimit = 8 * 1024 * 1024,
): Promise<ScriptOutput> {
	if (Buffer.byteLength(input) > inputLimit) {
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
		if (Buffer.byteLength(JSON.stringify(output)) > outputLimit) {
			return { ok: false, diagnostics: [], errors: [`response exceeds ${outputLimit} byte limit`] };
		}
		return output;
	} catch (error) {
		return { ok: false, diagnostics: [], errors: [messageOf(error)] };
	}
}

export function unicodeColumn(sourceLine: string, utf16Column: number): number {
	const index = Math.max(0, Math.min(sourceLine.length, utf16Column - 1));
	return [...sourceLine.slice(0, index)].length + 1;
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function isLocation(value: unknown): value is Location {
	return isRecord(value) && Number.isInteger(value.row) && Number(value.row) >= 1 && Number.isInteger(value.column)
		&& Number(value.column) >= 1;
}
function isDiagnostic(value: unknown): value is Diagnostic {
	return isRecord(value) && typeof value.filename === 'string' && typeof value.code === 'string'
		&& typeof value.message === 'string' && isLocation(value.location)
		&& (value.severity === undefined || value.severity === 'error' || value.severity === 'warning'
			|| value.severity === 'info')
		&& (value.end_location === undefined || isLocation(value.end_location))
		&& (value.fixes === undefined || (Array.isArray(value.fixes) && value.fixes.every(isFix)));
}
function isFix(value: unknown): value is Fix {
	if (!isRecord(value) || (value.description !== undefined && typeof value.description !== 'string')) return false;
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
async function readStdin(limit: number): Promise<string> {
	const chunks: Buffer[] = [];
	let size = 0;
	for await (const chunk of process.stdin) {
		const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
		size += bytes.byteLength;
		if (size > limit) throw new Error(`request exceeds ${limit} byte limit`);
		chunks.push(bytes);
	}
	return Buffer.concat(chunks).toString('utf8');
}
function writeJSON(output: ScriptOutput, limit: number): void {
	const text = JSON.stringify(output);
	if (Buffer.byteLength(text) > limit) {
		process.stdout.write(
			JSON.stringify({ ok: false, diagnostics: [], errors: [`response exceeds ${limit} byte limit`] }),
		);
		return;
	}
	process.stdout.write(text);
}
