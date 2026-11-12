import { compileProvider } from '#authoring';
import type { Diagnostic, JSONValue, ScriptRequest } from '#index';
import { isDiagnostic, isJSONValue } from '#index';
import { array, record, runCommand, text } from '#process';

/** Select the actionlint executable that validates this plugin. */
export interface AuthoringOptions {
	/** kjanat/actionlint executable. Defaults to ACTIONLINT, then actionlint on PATH. */
	actionlint?: string;
	/** Bound the entire validation/test subprocess; defaults to 30 seconds. */
	timeoutMs?: number;
}

/** Decoded scanner output, before workflow policy changes diagnostic severity. */
export interface ProviderResult {
	/** Findings after actionlint validates ranges, fixes and coordinate conventions. */
	diagnostics: Diagnostic[];
	/** Additional source names; permitted only when the manifest declares that capability. */
	sources?: string[];
	/** Scanner-reported failures, distinct from findings about the captured source. */
	errors?: string[];
}

/** Interpreter and environment facts used to evaluate a preparation transform. */
export interface PreparationContext {
	language: string;
	startup?: string;
	environment_startup?: string;
	environment_known?: boolean;
	config_file?: string;
	filesystem?: boolean;
	engine?: boolean;
	input?: boolean;
}

/** Generated text and tool options returned by a preparation transform. */
export interface PreparationPlan {
	source_prefix?: string;
	startup?: string;
	config?: JSONValue;
	variables?: Record<string, string>;
	excluded?: string[];
	skip?: string;
}

interface CaseInput {
	/** Required when the provider defines preparationTransform. Source and settings come from request. */
	preparation?: PreparationContext;
	/** Unique label shown by the CLI and assertion failure, such as "PowerShell alias". */
	name: string;
	/** Captured snippet/document and user configuration. The linter is never executed. */
	request: ScriptRequest & { directory?: string };
	/** Preparation variables exposed to jq as $vars; actionlint filename and exit-code values take precedence. */
	variables?: Record<string, string>;
	/** Recorded process status; defaults to zero and must match the manifest's accepted exit codes. */
	exit_code?: number;
}

interface SuccessAssertions {
	/** Complete source sent to the process after generated text is inserted. */
	expect_source?: string;
	/** Complete plan returned by the preparation transform. */
	expect_preparation?: PreparationPlan;
	/** Recorded stdout exactly as emitted by the wrapped linter. An empty string still gets decoded. */
	stdout?: string;
	/** Expected complete diagnostics/result after actionlint decodes output and validates source ranges. */
	expect?: ProviderResult;
	/** Expected configuration after the manifest's jq config transform. */
	expect_config?: JSONValue;
	/** Expected complete variable map after configuration overrides and host variables. */
	expect_vars?: Record<string, string>;
	expect_error?: never;
}

/**
 * One recorded scanner/configuration example with at least one assertion.
 * The expect_error assertion matches errors returned by actionlint.
 *
 * @example
 * ```ts
 * const clean: ProviderTestCase = {
 *   name: 'clean output', request: { source: 'echo ok\n', filename: 'script.sh' },
 *   stdout: '[]', expect: { diagnostics: [] },
 * };
 * ```
 */
export type ProviderTestCase =
	& CaseInput
	& (
		| SuccessAssertions & { stdout: string; expect: ProviderResult }
		| SuccessAssertions & { expect_source: string }
		| SuccessAssertions & { expect_preparation: PreparationPlan }
		| SuccessAssertions & { expect_config: JSONValue }
		| SuccessAssertions & { expect_vars: Record<string, string> }
		| {
			stdout?: string;
			/** Substring required in a genuine config/decoder failure, such as "invalid jq transform output". */
			expect_error: string;
			expect?: never;
			expect_source?: never;
			expect_preparation?: never;
			expect_config?: never;
			expect_vars?: never;
		}
	);

/** Result of actionlint validating the manifest and bundled schemas and compiling jq transforms. */
export interface ValidationReport {
	/** Version of the authoring report protocol. */
	schema_version: 1;
	/** True only when actionlint accepts the process manifest and compiles its transforms. */
	valid: boolean;
	/** Parse, schema or compiler error from actionlint when validation fails. */
	error?: string;
}

/** One evaluated fixture, including the values needed to diagnose failed assertions. */
export interface CaseReport {
	/** Source after generated comments and startup commands are inserted. */
	source: string;
	/** The submitted fixture's name. */
	name: string;
	/** Whether all assertions passed, including any explicitly expected pipeline error. */
	passed: boolean;
	/** Configuration computed by actionlint after transformation. */
	config?: JSONValue;
	/** Argument and jq variables computed by actionlint. */
	variables?: Record<string, string>;
	/** Decoded findings and scanner errors; omitted for configuration-only fixtures. */
	result?: ProviderResult;
	/** Pipeline or expectation failure, if any. */
	error?: string;
}

/** Machine-readable result of validating the manifest and evaluating all submitted fixtures. */
export interface TestReport extends ValidationReport {
	/** Fixture results in declaration order; never interpret an empty suite as a passing test. */
	cases: CaseReport[];
}

/** Failed author assertions with the complete actionlint report for test-runner diagnostics. */
export class ProviderTestError extends Error {
	/** The cases evaluated by actionlint and its validation error. */
	readonly report: TestReport;

	constructor(report: TestReport) {
		const failures = report.cases.filter((item) => !item.passed)
			.map((item) => `${item.name}: ${item.error ?? 'assertion failed'}`);
		super(report.error ?? (failures.join('\n') || 'provider tests failed'));
		this.name = 'ProviderTestError';
		this.report = report;
	}
}

/**
 * Validate compiled manifest data with actionlint's own schema and jq compiler.
 * No scanner is launched, environment read by jq, or network access performed.
 *
 * @example
 * ```ts
 * import { compileProvider } from '@kjanat-actionlint/plugin-sdk/authoring';
 * import { validateManifest } from '@kjanat-actionlint/plugin-sdk/testing';
 * const manifest = await compileProvider(new URL('./actionlint.plugin.ts', import.meta.url));
 * const report = await validateManifest(manifest);
 * if (!report.valid) throw new Error(report.error);
 * ```
 */
export async function validateManifest(
	manifest: Record<string, JSONValue>,
	options: AuthoringOptions = {},
): Promise<ValidationReport> {
	const response = await invokeHost(['plugin', 'validate', '-', '--json'], manifest, options);
	return validationReport(response);
}

/**
 * Validate a compiled manifest and evaluate fixtures loaded from JSON.
 * actionlint validates the fixture structure and rejects empty suites and missing assertions.
 * Use createPluginTester for typed source fixtures and automatic manifest compilation.
 *
 * @example
 * ```ts
 * const fixtures: unknown = JSON.parse(await readFile('actionlint.fixtures.json', 'utf8'));
 * const report = await testManifest(manifest, fixtures);
 * if (!report.valid) throw new ProviderTestError(report);
 * ```
 */
export async function testManifest(
	manifest: Record<string, JSONValue>,
	cases: unknown,
	options: AuthoringOptions = {},
): Promise<TestReport> {
	if (!isJSONValue(cases)) throw new Error('manifest fixtures must contain JSON-compatible data');
	const response = await invokeHost(['plugin', 'test', '--json'], { manifest, cases }, options);
	return testReport(response);
}

/** Framework-independent test client; use assert with Bun, Node, Deno, Vitest or another test runner. */
export interface PluginTester {
	/** Load the source definition and embed its jq/program without changing author package files. */
	compile(): Promise<Record<string, JSONValue>>;
	/** Validate the manifest and compile all jq expressions without invoking the wrapped tool. */
	validate(): Promise<ValidationReport>;
	/** Evaluate recorded fixtures with actionlint and return all successes and failures. */
	test(cases: readonly ProviderTestCase[]): Promise<TestReport>;
	/** Evaluate fixtures and throw ProviderTestError when any assertion or validation fails. */
	assert(cases: readonly ProviderTestCase[]): Promise<TestReport>;
}

/**
 * Test a source manifest and its real jq expressions against recorded scanner output.
 * Only author-controlled TypeScript is imported; plugin executables are never launched.
 * actionlint uses the same jq sandbox, schemas, coordinate conversion and fix checks as linting.
 *
 * @example
 * ```ts
 * import { test } from 'bun:test';
 * import { createPluginTester } from '@kjanat-actionlint/plugin-sdk/testing';
 * const plugin = createPluginTester(new URL('./actionlint.plugin.ts', import.meta.url));
 * test('clean scanner output', async () => {
 *   await plugin.assert([{
 *     name: 'clean', request: { source: 'echo ok\n', filename: 'script.sh' },
 *     stdout: '[]', expect: { diagnostics: [] },
 *   }]);
 * });
 * ```
 */
export function createPluginTester(definition: string | URL, options: AuthoringOptions = {}): PluginTester {
	const compile = () => compileProvider(definition);
	const test = async (cases: readonly ProviderTestCase[]): Promise<TestReport> => {
		if (cases.length === 0) throw new Error('provide at least one manifest test case');
		const manifest = await compile();
		return testManifest(manifest, cases, options);
	};
	return {
		compile,
		validate: async () => validateManifest(await compile(), options),
		test,
		assert: async (cases) => {
			const report = await test(cases);
			if (!report.valid) throw new ProviderTestError(report);
			return report;
		},
	};
}

async function invokeHost(args: string[], input: unknown, options: AuthoringOptions): Promise<unknown> {
	const executable = options.actionlint ?? process.env.ACTIONLINT ?? 'actionlint';
	let output: Awaited<ReturnType<typeof runCommand>>;
	try {
		output = await runCommand(executable, args, {
			stdin: JSON.stringify(input),
			allowedExitCodes: [0, 1],
			timeoutMs: options.timeoutMs ?? 30_000,
		});
	} catch (error) {
		throw new Error(
			`Cannot run kjanat/actionlint's plugin authoring commands. Install a host with plugin validate/test support or select it with ACTIONLINT or the actionlint option. ${
				error instanceof Error ? error.message : String(error)
			}`,
		);
	}
	let response: unknown;
	try {
		response = JSON.parse(output.stdout);
	} catch {
		throw new Error(
			'actionlint did not return an authoring report; use kjanat/actionlint with plugin validate/test support',
		);
	}
	const report = validationReport(response);
	if ((output.exitCode === 0) !== report.valid) {
		throw new Error('actionlint authoring report disagrees with its exit status');
	}
	return response;
}

function validationReport(value: unknown): ValidationReport {
	const object = record(value, 'authoring report');
	if (object.schema_version !== 1 || typeof object.valid !== 'boolean') {
		throw new Error('unsupported actionlint authoring report; expected schema_version 1');
	}
	return {
		schema_version: 1,
		valid: object.valid,
		...(object.error === undefined ? {} : { error: text(object.error, 'authoring report.error') }),
	};
}

function testReport(value: unknown): TestReport {
	const object = record(value, 'test report');
	const report = validationReport(object);
	const cases = array(object.cases, 'test report.cases').map((value): CaseReport => {
		const item = record(value, 'case report');
		if (typeof item.passed !== 'boolean') throw new Error('case report.passed must be boolean');
		if (typeof item.source !== 'string') throw new Error('case report.source must be a string');
		if (item.config !== undefined && !isJSONValue(item.config)) throw new Error('case report.config must be JSON');
		return {
			source: item.source,
			name: text(item.name, 'case report.name'),
			passed: item.passed,
			...(item.config === undefined ? {} : { config: item.config }),
			...(item.variables === undefined ? {} : { variables: variables(item.variables) }),
			...(item.result === undefined ? {} : { result: providerResult(item.result) }),
			...(item.error === undefined ? {} : { error: text(item.error, 'case report.error') }),
		};
	});
	if (report.valid && (cases.length === 0 || cases.some((item) => !item.passed))) {
		throw new Error('actionlint reported a passing suite without passing fixture results');
	}
	return { ...report, cases };
}

function variables(value: unknown): Record<string, string> {
	return Object.fromEntries(
		Object.entries(record(value, 'case report.variables')).map(([key, item]) => {
			if (typeof item !== 'string') throw new Error(`case report.variables.${key} must be a string`);
			return [key, item];
		}),
	);
}

function providerResult(value: unknown): ProviderResult {
	const object = record(value, 'case report.result');
	const diagnostics = array(object.diagnostics, 'case report.result.diagnostics');
	if (!diagnostics.every(isDiagnostic)) throw new Error('case report contains an invalid diagnostic');
	return {
		diagnostics,
		...(object.sources === undefined ? {} : { sources: strings(object.sources, 'result.sources') }),
		...(object.errors === undefined ? {} : { errors: strings(object.errors, 'result.errors') }),
	};
}

function strings(value: unknown, label: string): string[] {
	return array(value, label).map((item) => {
		if (typeof item !== 'string') throw new Error(`${label} must contain strings`);
		return item;
	});
}
