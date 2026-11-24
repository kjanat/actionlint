import { compileProvider } from '#authoring';
import type { Diagnostic, JSONValue, ScriptRequest } from '#index';
import { isDiagnostic, isJSONValue } from '#index';
import { array, record, runCommand, text } from '#process';

export interface AuthoringOptions {
	actionlint?: string;
	timeoutMs?: number;
}

export interface ProviderResult {
	diagnostics: Diagnostic[];
	sources?: string[];
	errors?: string[];
}

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

export interface PreparationPlan {
	source_prefix?: string;
	startup?: string;
	config?: JSONValue;
	variables?: Record<string, string>;
	excluded?: string[];
	skip?: string;
}

interface CaseInput {
	/** Required when the provider defines preparationTransform. */
	preparation?: PreparationContext;
	/** Unique label shown by the CLI and assertion failures. */
	name: string;
	request: ScriptRequest & { directory?: string };
	variables?: Record<string, string>;
	/** Recorded process status; defaults to zero and must match the manifest's accepted exit codes. */
	exit_code?: number;
}

interface SuccessAssertions {
	/** Complete source sent to the process after generated text is inserted. */
	expect_source?: string;
	expect_preparation?: PreparationPlan;
	stdout?: string;
	expect?: ProviderResult;
	expect_config?: JSONValue;
	expect_vars?: Record<string, string>;
	expect_error?: never;
}

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
			expect_error: string;
			expect?: never;
			expect_source?: never;
			expect_preparation?: never;
			expect_config?: never;
			expect_vars?: never;
		}
	);

export interface ValidationReport {
	schema_version: 1;
	valid: boolean;
	error?: string;
}

export interface CaseReport {
	source: string;
	name: string;
	passed: boolean;
	config?: JSONValue;
	variables?: Record<string, string>;
	/** Omitted for configuration-only fixtures. */
	result?: ProviderResult;
	error?: string;
}

export interface TestReport extends ValidationReport {
	/** Fixture results in declaration order. */
	cases: CaseReport[];
}

export class ProviderTestError extends Error {
	readonly report: TestReport;

	constructor(report: TestReport) {
		const failures = report.cases.filter((item) => !item.passed)
			.map((item) => `${item.name}: ${item.error ?? 'assertion failed'}`);
		super(report.error ?? (failures.join('\n') || 'provider tests failed'));
		this.name = 'ProviderTestError';
		this.report = report;
	}
}

export async function validateManifest(
	manifest: Record<string, JSONValue>,
	options: AuthoringOptions = {},
): Promise<ValidationReport> {
	const response = await invokeHost(['plugin', 'validate', '-', '--json'], manifest, options);
	return validationReport(response);
}

export async function testManifest(
	manifest: Record<string, JSONValue>,
	cases: unknown,
	options: AuthoringOptions = {},
): Promise<TestReport> {
	if (!isJSONValue(cases)) throw new Error('manifest fixtures must contain JSON-compatible data');
	const response = await invokeHost(['plugin', 'test', '--json'], { manifest, cases }, options);
	return testReport(response);
}

export interface PluginTester {
	compile(): Promise<Record<string, JSONValue>>;
	validate(): Promise<ValidationReport>;
	test(cases: readonly ProviderTestCase[]): Promise<TestReport>;
	/** Throws ProviderTestError when any assertion or validation fails. */
	assert(cases: readonly ProviderTestCase[]): Promise<TestReport>;
}

/**
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
