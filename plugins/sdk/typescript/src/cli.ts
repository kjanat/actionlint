#!/usr/bin/env node
import { constants } from 'node:fs';
import { access, mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, extname, join, resolve } from 'node:path';

import { arg, cli, command, flag } from '@kjanat/dreamcli';

import { compileProvider } from './authoring.ts';
import type { JSONValue } from './index.ts';
import { isJSONValue } from './index.ts';
import type { AuthoringOptions } from './testing.ts';
import { testManifest, validateManifest } from './testing.ts';

export const initCommand = command('init')
	.description('Create a typed manifest, jq transform and fixtures while preserving existing project settings')
	.arg(
		'directory',
		arg.path({ type: 'directory', mustExist: false }).default('.').describe('Directory for the new provider package'),
	)
	.flag('sdk', flag.string().describe('SDK dependency specifier: version, preview URL, workspace:* or file: tarball'))
	.example(({ name }) => `${name} init ./plugins/my-check`, 'Create a plugin in a new directory')
	.example(({ name }) => `${name} init`, 'Initialize the current directory')
	.example(({ name }) => `${name} init --sdk file:./plugin-sdk.tgz`, 'Use a locally packed SDK')
	.action(async ({ args, flags, out }) => {
		const directory = await scaffold(args.directory, flags.sdk);
		if (out.jsonMode) out.json({ directory });
		else out.status(`Created plugin scaffold in ${directory}`);
	});

export const packCommand = command('pack')
	.description('Bundle a provider and write its registry-ready process manifest')
	.arg(
		'manifest',
		arg.path({ type: 'file' }).default('actionlint.plugin.ts').describe('TypeScript provider definition'),
	)
	.flag(
		'actionlint',
		flag.string().env('ACTIONLINT').describe(
			'kjanat/actionlint executable for validation (default: actionlint on PATH)',
		),
	)
	.example(({ name }) => `${name} pack`, 'Pack actionlint.plugin.ts in the current directory')
	.example(({ name }) => `${name} pack ./plugins/my-check/actionlint.plugin.ts`, 'Pack a specific provider')
	.action(async ({ args, flags, out }) => {
		const manifest = await pack(args.manifest, { actionlint: flags.actionlint });
		if (out.jsonMode) out.json({ manifest });
		else out.status(`Wrote ${manifest}`);
	});

export const validateCommand = command('validate')
	.description('Validate a TypeScript or JSON manifest and compile jq with the actual actionlint host')
	.arg(
		'manifest',
		arg.path({ type: 'file' }).default('actionlint.plugin.ts').describe('Authored definition or packed JSON manifest'),
	)
	.flag(
		'actionlint',
		flag.string().env('ACTIONLINT').describe('kjanat/actionlint executable (default: actionlint on PATH)'),
	)
	.example(({ name }) => `${name} validate`, 'Validate the source definition and embedded jq files')
	.example(({ name }) => `${name} validate actionlint-provider.json`, 'Check a distributable manifest')
	.action(async ({ args, flags, out }) => {
		const report = await validateManifest(await authoredManifest(args.manifest), { actionlint: flags.actionlint });
		if (out.jsonMode) out.json(report);
		else if (report.valid) out.status('Manifest and jq transforms are valid');
		else out.error(report.error ?? 'manifest validation failed');
		if (!report.valid) out.setExitCode(1);
	});

export const testCommand = command('test')
	.description('Test jq/configuration against recorded fixtures without launching the scanner')
	.arg(
		'manifest',
		arg.path({ type: 'file' }).default('actionlint.plugin.ts').describe('Authored definition or packed JSON manifest'),
	)
	.arg(
		'fixtures',
		arg.path({ type: 'file' }).default('actionlint.fixtures.json').describe(
			'JSON array of named request/stdout/expect fixtures',
		),
	)
	.flag(
		'actionlint',
		flag.string().env('ACTIONLINT').describe('kjanat/actionlint executable (default: actionlint on PATH)'),
	)
	.example(({ name }) => `${name} test`, 'Run the scaffolded manifest fixtures')
	.example(
		({ name }) => `${name} test actionlint.plugin.ts fixtures/scanner.json --json`,
		'Return a machine-readable fixture report',
	)
	.action(async ({ args, flags, out }) => {
		const fixtures: unknown = JSON.parse(await readFile(args.fixtures, 'utf8'));
		const report = await testManifest(await authoredManifest(args.manifest), fixtures, {
			actionlint: flags.actionlint,
		});
		if (out.jsonMode) out.json(report);
		else {
			for (const item of report.cases) {
				out.log(`${item.passed ? 'PASS' : 'FAIL'} ${item.name}${item.error ? `: ${item.error}` : ''}`);
			}
			if (report.error) out.error(report.error);
			out.status(`${report.cases.filter((item) => item.passed).length}/${report.cases.length} fixtures passed`);
		}
		if (!report.valid) out.setExitCode(1);
	});

export const pluginCLI = cli('actionlint-plugin')
	.description('Create and package actionlint plugins')
	.command(initCommand)
	.command(packCommand)
	.command(validateCommand)
	.command(testCommand);

if (import.meta.main) await pluginCLI.run();

async function pack(manifestPath: string, options: AuthoringOptions): Promise<string> {
	const file = resolve(manifestPath);
	const root = dirname(file);
	const manifest = await compileProvider(file);
	const manifestText = `${JSON.stringify(manifest, null, 2)}\n`;
	if (Buffer.byteLength(manifestText) > 1024 * 1024) {
		throw new Error('generated provider manifest exceeds the host 1 MiB limit');
	}
	const validation = await validateManifest(manifest, options);
	if (!validation.valid) throw new Error(validation.error ?? 'host rejected the provider manifest');
	const packageFile = join(root, 'package.json');
	const packageData = parseObject(await readFile(packageFile, 'utf8'), 'package.json');
	if (typeof packageData.name !== 'string' || !isNpmName(packageData.name)) {
		throw new Error('package.json must declare a valid package name');
	}
	const spec = manifest.process;
	if (!isRecord(spec)) throw new Error('compiled manifest has no process specification');
	const program = spec.program;
	const bundled = isRecord(program) && typeof program.inline === 'string';
	const bundleFile = bundled ? (packageData.type === 'module' ? 'actionlint.js' : 'actionlint.mjs') : undefined;
	let packageChanged = false;
	if (bundleFile !== undefined) {
		// Adding an exports map to a legacy package would hide its existing main
		// entry and subpaths. Extend only maps the author already declared.
		if (packageData.exports !== undefined) packageData.exports = withActionlintExport(packageData.exports, bundleFile);
		packageData.bin = withPluginBin(packageData.bin, packageData.name, bundleFile);
		packageChanged = true;
	}
	if (packageData.files !== undefined) {
		packageData.files = withPublishedFiles(packageData.files, bundleFile);
		packageChanged = true;
	}
	// Validate with actionlint before writing to the author's package.
	if (bundleFile !== undefined && isRecord(program) && typeof program.inline === 'string') {
		await writeFile(join(root, bundleFile), program.inline);
	}
	if (packageChanged) await writeFile(packageFile, `${JSON.stringify(packageData, null, 2)}\n`);
	const manifestFile = join(root, 'actionlint-provider.json');
	await writeFile(manifestFile, manifestText);
	return manifestFile;
}

async function authoredManifest(path: string): Promise<Record<string, JSONValue>> {
	if (extname(path).toLowerCase() === '.json') {
		return parseObject(await readFile(path, 'utf8'), path);
	}
	return compileProvider(path);
}

async function scaffold(directory: string, sdkSpecifier: string | undefined): Promise<string> {
	const root = resolve(directory);
	await mkdir(root, { recursive: true });
	const packageBefore = await optionalText(join(root, 'package.json'));
	const ignoreBefore = await optionalText(join(root, '.gitignore'));
	const readmeBefore = await optionalText(join(root, 'README.md'));
	const packageData = packageBefore === undefined
		? {
			name: '@example/actionlint-plugin-example',
			version: '0.1.0',
			type: 'module',
			files: ['actionlint-provider.json'],
		}
		: parseObject(packageBefore, 'package.json');
	const packageContents = await scaffoldPackage(packageData, sdkSpecifier);
	const files = new Map<string, string>([
		['package.json', `${JSON.stringify(packageContents, null, 2)}\n`],
		[
			'actionlint.plugin.ts',
			`import { defineProcessProvider, jqOutput } from '@kjanat-actionlint/plugin-sdk';

export default defineProcessProvider({
  id: 'example/scanner',
  version: '0.1.0',
  name: 'Example scanner',
  description: 'Map a scanner JSON result to actionlint diagnostics.',
  license: 'MIT',
  repository: 'https://github.com/example/actionlint-plugin-example',
  shells: ['bash'],
  rule: { name: 'example-check', category: 'correctness', subcategory: 'shell', maturity: 'nursery', description: 'Scanner findings', provider: 'example/scanner' },
  entry: 'example-linter',
  args: ['--format', 'json'],
  stdin: 'source',
  config: { render: 'none' },
  output: { format: 'json', transform: jqOutput('./diagnostics.jq') }
});
`,
		],
		[
			'diagnostics.jq',
			`map({
  filename: $vars.filename,
  code: .code,
  message: .message,
  severity: (.severity // "warning"),
  location: {row: .line, column: .column}
})
`,
		],
		[
			'actionlint.fixtures.json',
			`${
				JSON.stringify(
					[
						{
							name: 'scanner finding',
							request: { source: 'echo ok\n', filename: 'script.sh' },
							stdout: JSON.stringify([{ code: 'EXAMPLE001', message: 'Example finding', line: 1, column: 1 }]),
							expect: {
								diagnostics: [{
									filename: 'script.sh',
									code: 'EXAMPLE001',
									message: 'Example finding',
									severity: 'warning',
									location: { row: 1, column: 1 },
								}],
							},
						},
						{
							name: 'clean scanner output',
							request: { source: 'echo ok\n', filename: 'script.sh' },
							stdout: '[]',
							expect: { diagnostics: [] },
						},
					],
					null,
					2,
				)
			}\n`,
		],
		['.gitignore', pluginIgnore(ignoreBefore)],
		[
			'README.md',
			`# Example actionlint plugin

This package contains a typed process manifest, its jq transform, and recorded-output fixtures. Replace the example identity, executable/arguments and jq mapping with your scanner's contract. The example scanner need not be installed to validate or test recorded fixtures.

Install the package dependencies and a kjanat/actionlint host with \`plugin validate\` / \`plugin test\` support. Existing SDK dependency specifiers are preserved, including preview URLs and \`workspace:*\`. For an unpublished SDK, initialize with \`actionlint-plugin init --sdk file:./plugin-sdk.tgz\` or install the preview SDK in this package before initializing. Set \`ACTIONLINT\` or pass \`--actionlint\` to choose the host executable.

Run \`bun run actionlint-plugin validate\`, \`bun run actionlint-plugin test\`, then \`bun run actionlint-plugin pack\`. Tests execute actionlint's own jq engine without launching the scanner. Packing validates before writing \`actionlint-provider.json\`, which embeds the jq source and registry metadata. It does not publish a registry release.

Users need the wrapped scanner on PATH. Repository-selected native processes require \`actionlint --allow-config-processes\`; explicit invocation authorizes a provider with \`--plugin\`. This is native execution under the user's OS privileges, not a sandbox. See the SDK's author guide for source tests, configuration transforms, callback runtimes and publication.
`,
		],
	]);
	if (readmeBefore !== undefined) files.delete('README.md');
	const existing = new Set([
		...(packageBefore === undefined ? [] : ['package.json']),
		...(ignoreBefore === undefined ? [] : ['.gitignore']),
	]);
	for (const [name] of files) {
		if (existing.has(name)) continue;
		try {
			await access(join(root, name), constants.F_OK);
			throw new Error(`refusing to overwrite ${join(root, name)}`);
		} catch (error) {
			if (!isNotFound(error)) throw error;
		}
	}
	for (const [name, contents] of files) {
		await writeFile(join(root, name), contents, { flag: existing.has(name) ? 'w' : 'wx' });
	}
	return root;
}

async function scaffoldPackage(
	existing: Record<string, JSONValue>,
	sdkSpecifier: string | undefined,
): Promise<Record<string, JSONValue>> {
	const sdk = '@kjanat-actionlint/plugin-sdk';
	const dependencies = packageSection(existing, 'dependencies');
	const devDependencies = packageSection(existing, 'devDependencies');
	if (sdkSpecifier !== undefined && sdkSpecifier.trim() === '') throw new Error('--sdk must not be empty');
	if (sdk in dependencies || sdk in devDependencies) {
		for (const section of ['dependencies', 'devDependencies']) {
			const values = packageSection(existing, section);
			if (!(sdk in values)) continue;
			if (typeof values[sdk] !== 'string') throw new Error(`package.json ${section}.${sdk} must be a string`);
			if (sdkSpecifier !== undefined) existing[section] = { ...values, [sdk]: sdkSpecifier };
		}
	} else {
		const metadata = parseObject(
			await readFile(new URL('../package.json', import.meta.url), 'utf8'),
			'SDK package.json',
		);
		if (typeof metadata.version !== 'string') throw new Error('SDK package.json has no version');
		existing.devDependencies = { ...devDependencies, [sdk]: sdkSpecifier ?? `^${metadata.version}` };
	}
	const scripts = packageSection(existing, 'scripts');
	existing.scripts = {
		'plugin:validate': 'actionlint-plugin validate',
		'plugin:test': 'actionlint-plugin test',
		'plugin:pack': 'actionlint-plugin pack',
		...scripts,
	};
	if (existing.files !== undefined) existing.files = withPublishedFiles(existing.files);
	return existing;
}

function packageSection(data: Record<string, JSONValue>, name: string): Record<string, JSONValue> {
	const section = data[name];
	if (section === undefined) return {};
	if (!isRecord(section)) throw new Error(`package.json ${name} must be an object`);
	return section;
}

async function optionalText(path: string): Promise<string | undefined> {
	try {
		return await readFile(path, 'utf8');
	} catch (error) {
		if (!isNotFound(error)) throw error;
		return undefined;
	}
}

function pluginIgnore(existing: string | undefined): string {
	const before = existing ?? '';
	const lines = new Set(before.split(/\r?\n/u));
	const added = ['node_modules/', 'actionlint-provider.json', 'actionlint.js', 'actionlint.mjs', '*.tgz'].filter((
		line,
	) => !lines.has(line));
	if (added.length === 0) return before;
	return before + (before !== '' && !before.endsWith('\n') ? '\n' : '') + added.map((line) => `${line}\n`).join('');
}

function isRecord(value: unknown): value is Record<string, JSONValue> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isNpmName(value: string): boolean {
	return /^(@[a-z0-9][a-z0-9._-]*\/)?[a-z0-9][a-z0-9._-]*$/u.test(value);
}

function withActionlintExport(existing: JSONValue, filename: string): Record<string, JSONValue> {
	const subpath = './actionlint';
	const target = `./${filename}`;
	if (existing === null || typeof existing === 'string' || Array.isArray(existing)) {
		return { '.': existing, [subpath]: target };
	}
	if (!isRecord(existing)) throw new Error('package.json exports must be a string, array, or object');
	const keys = Object.keys(existing);
	const hasSubpath = keys.some((key) => key.startsWith('.'));
	if (!hasSubpath) return { '.': existing, [subpath]: target };
	if (keys.some((key) => !key.startsWith('.'))) {
		throw new Error('package.json exports mixes subpath and condition keys');
	}
	if (existing[subpath] === target) return existing;
	if (subpath in existing) throw new Error('package.json already exports ./actionlint');
	return { ...existing, [subpath]: target };
}

function withPluginBin(
	existing: JSONValue | undefined,
	packageName: string,
	filename: string,
): Record<string, JSONValue> {
	const previous = typeof existing === 'string'
		? { [packageName.replace(/^@[^/]+\//u, '')]: existing }
		: existing === undefined
		? {}
		: existing;
	if (!isRecord(previous)) throw new Error('package.json bin must be a string or object');
	const name = executableName(packageName);
	const target = `./${filename}`;
	if (name in previous && previous[name] !== target) throw new Error(`package.json already declares bin ${name}`);
	return { ...previous, [name]: target };
}

function withPublishedFiles(existing: JSONValue, bundleFile?: string): JSONValue[] {
	if (!Array.isArray(existing) || !existing.every((file) => typeof file === 'string')) {
		throw new Error('package.json files must be an array of strings');
	}
	const files = [...existing];
	for (const file of [...(bundleFile === undefined ? [] : [bundleFile]), 'actionlint-provider.json']) {
		if (!files.some((entry) => entry.replace(/^(?:\.\/|\/)+/u, '') === file)) files.push(file);
	}
	return files;
}

function executableName(packageName: string): string {
	const name = packageName.replace(/^@[^/]+\//, '').replace(/^(?:actionlint-)?plugin-(?=.)/, '');
	const executable = `actionlint-plugin-${name.replace(/[^a-zA-Z0-9-]/g, '-')}`;
	if (executable.length > 214) throw new Error('package name is too long for a generated executable');
	return executable;
}

function parseObject(text: string, name: string): Record<string, JSONValue> {
	const value: unknown = JSON.parse(text);
	if (!isRecord(value) || !isJSONValue(value)) throw new Error(`${name} must contain a JSON object`);
	return value;
}

function isNotFound(error: unknown): boolean {
	return typeof error === 'object' && error !== null && 'code' in error && error.code === 'ENOENT';
}
