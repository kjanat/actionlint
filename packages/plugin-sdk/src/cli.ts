import { build } from 'esbuild';
import { access, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import type { JSONValue, PluginProvider, ProcessProvider, RuleDescriptor } from './index.ts';

const [command, ...args] = process.argv.slice(2);

try {
	if (command === 'init') await scaffold(args[0] ?? '.');
	else if (command === 'pack') await pack(args[0] ?? 'actionlint.plugin.ts');
	else throw new Error('usage: actionlint-plugin init [directory] | pack [manifest.ts]');
} catch (error) {
	process.stderr.write(`${messageOf(error)}\n`);
	process.exitCode = 1;
}

async function pack(manifestPath: string): Promise<void> {
	const file = resolve(manifestPath);
	const root = dirname(file);
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-plugin-pack-'));
	try {
		const bundledProvider = join(temporary, 'provider.mjs');
		await build({ entryPoints: [file], bundle: true, platform: 'node', format: 'esm', outfile: bundledProvider });
		const loaded: unknown = await import(pathToFileURL(bundledProvider).href);
		if (!isRecord(loaded) || !isPluginProvider(loaded.default)) {
			throw new Error('manifest must default-export a valid SDK provider');
		}
		const provider = loaded.default;
		const packageFile = join(root, 'package.json');
		const packageData = parseObject(await readFile(packageFile, 'utf8'), 'package.json');
		if (typeof packageData.name !== 'string' || !isNpmName(packageData.name)) {
			throw new Error('package.json must declare a valid package name');
		}
		const executable = executableName(packageData.name);
		const processSpec = await processDefinition(provider, root, packageData.name, file);
		const metadata = {
			metadata_version: 1,
			id: provider.id,
			name: provider.name,
			description: provider.description,
			license: provider.license,
			repository: provider.repository,
			plugin_version: provider.version,
			transport: 'process',
		};
		const manifest = {
			schema_version: 1,
			metadata,
			id: provider.id,
			version: provider.version,
			role: 'script',
			scope: 'source',
			transport: 'process',
			preparation: 'kjanat:actionlint/expression-mask@0.1.0',
			rule: provider.rule,
			selectors: { shells: provider.shells, ...(provider.filename ? { filename: provider.filename } : {}) },
			capabilities: { fixes: provider.fixes ?? false, reported_sources: false },
			process: processSpec,
		};
		if (provider.kind === 'script') {
			packageData.exports = withActionlintExport(packageData.exports);
			if (packageData.files !== undefined) packageData.files = withPublishedFiles(packageData.files);
			packageData.bin = {
				...(isRecord(packageData.bin) ? packageData.bin : {}),
				[executable]: './actionlint.js',
			};
			await writeFile(packageFile, `${JSON.stringify(packageData, null, 2)}\n`);
		}
		await writeFile(join(root, 'actionlint-provider.json'), `${JSON.stringify(manifest, null, 2)}\n`);
		process.stdout.write('Wrote actionlint-provider.json\n');
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
}

async function processDefinition(
	provider: PluginProvider,
	root: string,
	packageName: string,
	manifestFile: string,
): Promise<Record<string, JSONValue>> {
	if (provider.kind === 'process') {
		const configTransform = provider.jqConfigFile ? await jqFile(root, provider.jqConfigFile) : undefined;
		const outputTransform = provider.jqOutputFile
			? await jqFile(root, provider.jqOutputFile)
			: await resolveJQ(provider.output.transform, root);
		return {
			entry: provider.entry,
			args: provider.args ?? [],
			stdin: provider.stdin,
			config: { render: provider.config.render, ...(configTransform ? { transform: configTransform } : {}) },
			output: { format: provider.output.format, ...(outputTransform ? { transform: outputTransform } : {}) },
			...(provider.env_passthrough ? { env_passthrough: provider.env_passthrough } : {}),
		};
	}
	const outfile = join(root, 'actionlint.js');
	const source = [
		`import provider from ${JSON.stringify(manifestFile)};`,
		`import { serveScriptEngine } from '@kjanat/actionlint-plugin-sdk';`,
		'await serveScriptEngine(provider.check);',
	].join('\n');
	await build({
		stdin: { contents: source, resolveDir: root, sourcefile: 'actionlint-plugin-entry.ts', loader: 'ts' },
		bundle: true,
		platform: 'node',
		target: 'node22',
		format: 'esm',
		outfile,
		banner: { js: '#!/usr/bin/env node' },
		external: provider.external ?? [],
	});
	return {
		entry: 'node',
		args: ['--input-type=module', '--eval', `await import(${JSON.stringify(`${packageName}/actionlint`)})`],
		stdin: 'request',
		config: { render: 'json-stdin' },
		output: {
			format: 'script-diagnostics@1',
			coordinates: { rows: 'lf', columns: 'chars', end: 'exclusive' },
		},
	};
}

async function resolveJQ(
	transform: ProcessProvider['output']['transform'],
	root: string,
): Promise<{ language: 'jq'; inline: string } | undefined> {
	if (transform === undefined) return undefined;
	if ('__jqFile' in transform && typeof transform.__jqFile === 'string') return jqFile(root, transform.__jqFile);
	if (isRecord(transform) && transform.language === 'jq' && typeof transform.inline === 'string') {
		return { language: 'jq', inline: transform.inline };
	}
	throw new Error('invalid jq transform');
}

async function jqFile(root: string, path: string): Promise<{ language: 'jq'; inline: string }> {
	const inline = await readFile(resolve(root, path), 'utf8');
	if (inline.trim().length === 0 || inline.length > 65536) {
		throw new Error(`jq transform ${path} must contain 1 to 65536 characters`);
	}
	return { language: 'jq', inline };
}

async function scaffold(directory: string): Promise<void> {
	const root = resolve(directory);
	await mkdir(root, { recursive: true });
	const files = new Map<string, string>([
		[
			'package.json',
			`${
				JSON.stringify(
					{
						name: '@example/actionlint-plugin-example',
						version: '0.1.0',
						type: 'module',
						dependencies: { '@kjanat/actionlint-plugin-sdk': '^0.1.0' },
						scripts: { pack: 'actionlint-plugin pack' },
					},
					null,
					2,
				)
			}\n`,
		],
		[
			'actionlint.plugin.ts',
			`import { defineScriptProvider } from '@kjanat/actionlint-plugin-sdk';

export default defineScriptProvider({
  id: 'example/typescript-check',
  version: '0.1.0',
  name: 'Example TypeScript check',
  description: 'Replace this scaffold with a real semantic check.',
  license: 'MIT',
  repository: 'https://github.com/example/actionlint-plugin-example',
  shells: ['bash'],
  rule: { name: 'example-check', category: 'correctness', subcategory: 'shell', maturity: 'nursery', description: 'Example check', provider: 'example/typescript-check' },
  check: () => [] // TODO: implement a real check.
});
`,
		],
		[
			'README.md',
			`# Example actionlint plugin

Requires Node.js 22.18 or newer and the SDK dependency. The SDK is not published yet; use a local \`file:\` dependency until it is. Run \`npm install\`, then \`npm run pack\`. Packing bundles the TypeScript manifest and its dependencies into the generated process executable and adds a package export. Install the plugin into the consuming Node project; actionlint resolves that export from the project's working directory. Packing writes the provider manifest with embedded registry metadata; it does not publish or create a registry release asset.

Repository-selected native processes require \`actionlint --allow-config-processes\`. Explicitly invoked registry plugins use \`--plugin\`.
`,
		],
	]);
	for (const [name] of files) {
		try {
			await access(join(root, name), constants.F_OK);
			throw new Error(`refusing to overwrite ${join(root, name)}`);
		} catch (error) {
			if (!isNotFound(error)) throw error;
		}
	}
	for (const [name, contents] of files) await writeFile(join(root, name), contents, { flag: 'wx' });
	process.stdout.write(`Created plugin scaffold in ${root}\n`);
}

function isPluginProvider(value: unknown): value is PluginProvider {
	if (!isRecord(value) || (value.kind !== 'script' && value.kind !== 'process')) return false;
	if (
		typeof value.id !== 'string' || typeof value.version !== 'string' || typeof value.name !== 'string'
		|| typeof value.description !== 'string' || typeof value.license !== 'string'
		|| typeof value.repository !== 'string' || !isRuleDescriptor(value.rule)
		|| !Array.isArray(value.shells) || !value.shells.every((shell) => typeof shell === 'string')
	) return false;
	if (value.kind === 'script') return typeof value.check === 'function';
	return typeof value.entry === 'string' && Array.isArray(value.args ?? [])
		&& (value.stdin === 'request' || value.stdin === 'source') && isRecord(value.config)
		&& typeof value.config.render === 'string' && isRecord(value.output) && typeof value.output.format === 'string';
}

function isRuleDescriptor(value: unknown): value is RuleDescriptor {
	return isRecord(value) && typeof value.name === 'string' && typeof value.category === 'string'
		&& typeof value.subcategory === 'string' && (value.maturity === 'stable' || value.maturity === 'nursery')
		&& typeof value.description === 'string' && typeof value.provider === 'string';
}

function isRecord(value: unknown): value is Record<string, JSONValue> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isNpmName(value: string): boolean {
	return /^(@[a-z0-9][a-z0-9._-]*\/)?[a-z0-9][a-z0-9._-]*$/u.test(value);
}

function withActionlintExport(existing: JSONValue | undefined): Record<string, JSONValue> {
	const subpath = './actionlint';
	if (existing === undefined) return { [subpath]: './actionlint.js' };
	if (typeof existing === 'string' || Array.isArray(existing)) {
		return { '.': existing, [subpath]: './actionlint.js' };
	}
	if (!isRecord(existing)) throw new Error('package.json exports must be a string, array, or object');
	const keys = Object.keys(existing);
	const hasSubpath = keys.some((key) => key.startsWith('.'));
	if (!hasSubpath) return { '.': existing, [subpath]: './actionlint.js' };
	if (keys.some((key) => !key.startsWith('.'))) {
		throw new Error('package.json exports mixes subpath and condition keys');
	}
	if (subpath in existing) throw new Error('package.json already exports ./actionlint');
	return { ...existing, [subpath]: './actionlint.js' };
}

function withPublishedFiles(existing: JSONValue): JSONValue[] {
	if (!Array.isArray(existing) || !existing.every((file) => typeof file === 'string')) {
		throw new Error('package.json files must be an array of strings');
	}
	return [...new Set([...existing, 'actionlint.js', 'actionlint-provider.json'])];
}

function executableName(packageName: string): string {
	const executable = `actionlint-plugin-${
		packageName.replace(/^@/, '').replaceAll('/', '-').replace(/[^a-zA-Z0-9-]/g, '-')
	}`;
	if (executable.length > 214) throw new Error('package name is too long for a generated executable');
	return executable;
}

function parseObject(text: string, name: string): Record<string, JSONValue> {
	const value: unknown = JSON.parse(text);
	if (!isRecord(value)) throw new Error(`${name} must contain a JSON object`);
	return value;
}

function isNotFound(error: unknown): boolean {
	return typeof error === 'object' && error !== null && 'code' in error && error.code === 'ENOENT';
}

function messageOf(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
