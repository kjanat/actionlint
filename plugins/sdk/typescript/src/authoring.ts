import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { createJiti } from 'jiti';
import { build } from 'tsdown';

import type {
	DocumentKind,
	DocumentProcessProvider,
	DocumentProvider,
	DocumentRequirement,
	JSONValue,
	PluginProvider,
	ProcessProvider,
	RuleDescriptor,
} from './index.ts';
import { isRuleCategory } from './index.ts';

/**
 * Compile an authored TypeScript definition into its distributable JSON manifest.
 * jq files resolve beside the definition and are embedded unchanged. Callback
 * bundling uses tsdown; no wrapped linter is executed and no package files change.
 * The trusted definition and its imports execute through jiti in ESM or CommonJS
 * projects, without retaining previous module values or writing a source cache.
 * Validate the result with validateManifest before distributing it.
 *
 * @example
 * ```ts
 * import { compileProvider } from '@kjanat-actionlint/plugin-sdk/authoring';
 * const manifest = await compileProvider(new URL('./actionlint.plugin.ts', import.meta.url));
 * ```
 */
export async function compileProvider(definition: string | URL): Promise<Record<string, JSONValue>> {
	const file = definition instanceof URL ? fileURLToPath(definition) : resolve(definition);
	const root = dirname(file);
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-plugin-compile-'));
	try {
		const load = createJiti(pathToFileURL(file).href, {
			fsCache: false,
			moduleCache: false,
			interopDefault: false,
			tryNative: false,
		});
		const provider = await load.import<unknown>(file, { default: true });
		if (!isPluginProvider(provider)) {
			throw new Error('manifest must default-export a valid SDK provider');
		}
		const processSpec = await processDefinition(provider, root, temporary, file);
		const documentProvider = isDocumentProvider(provider) ? provider : undefined;
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
			role: documentProvider ? 'document' : 'script',
			scope: 'source',
			transport: 'process',
			preparation: documentProvider ? 'generic' : 'kjanat:actionlint/expression-mask@0.1.0',
			rule: { ...provider.rule },
			selectors: selectorsFor(provider),
			capabilities: {
				fixes: provider.fixes ?? false,
				reported_sources: false,
				...(provider.resources ? { resources: provider.resources } : {}),
				...(provider.messageTemplate ? { message_template: provider.messageTemplate } : {}),
				...(provider.severities ? { severities: { ...provider.severities } } : {}),
			},
			...(provider.configSchema ? { config_schema: { ...provider.configSchema } } : {}),
			...(documentProvider ? { required_inputs: documentProvider.requiredInputs ?? ['source'] } : {}),
			process: processSpec,
		};

		if (Buffer.byteLength(JSON.stringify(manifest)) > 1024 * 1024) {
			throw new Error('generated provider manifest exceeds the host 1 MiB limit');
		}
		return manifest;
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
}

async function processDefinition(
	provider: PluginProvider,
	root: string,
	bundleDirectory: string,
	manifestFile: string,
): Promise<Record<string, JSONValue>> {
	if (provider.kind === 'process') {
		const configTransform = provider.jqConfigFile
			? await jqFile(root, provider.jqConfigFile)
			: await resolveJQ(provider.config.transform, root);
		const outputTransform = provider.jqOutputFile
			? await jqFile(root, provider.jqOutputFile)
			: await resolveJQ(provider.output.transform, root);
		return {
			entry: provider.entry,
			args: provider.args ?? [],
			stdin: provider.stdin,
			config: {
				render: provider.config.render,
				...(provider.config.forbidden ? { forbidden: provider.config.forbidden } : {}),
				...(configTransform ? { transform: configTransform } : {}),
			},
			output: {
				format: provider.output.format,
				...(provider.output.coordinates ? { coordinates: { ...provider.output.coordinates } } : {}),
				...(outputTransform ? { transform: outputTransform } : {}),
			},
			...(provider.env ? { env: { ...provider.env } } : {}),
			...(provider.env_unset ? { env_unset: provider.env_unset } : {}),
			...(provider.exit_codes ? { exit_codes: provider.exit_codes } : {}),
			...(provider.concurrency ? { concurrency: provider.concurrency } : {}),
			...(provider.versionProbe ? { version: { ...provider.versionProbe } } : {}),
			...(provider.env_passthrough ? { env_passthrough: provider.env_passthrough } : {}),
		};
	}
	const embeddedFile = join(bundleDirectory, 'actionlint.mjs');
	const documentProvider = isDocumentProvider(provider);
	const serveFunction = documentProvider ? 'serveDocumentEngine' : 'serveScriptEngine';
	const source = [
		`import provider from ${JSON.stringify(manifestFile)};`,
		`import { ${serveFunction} } from ${JSON.stringify(resolve(import.meta.dirname, 'stdio.js'))};`,
		`await ${serveFunction}(provider.check);`,
	].join('\n');
	const entry = join(bundleDirectory, 'entry.ts');
	await writeFile(entry, source);
	await build({
		entry: { actionlint: entry },
		outDir: bundleDirectory,
		config: false,
		clean: false,
		dts: false,
		platform: provider.bundle?.platform ?? 'neutral',
		target: 'esnext',
		format: 'esm',
		fixedExtension: true,
		deps: { alwaysBundle: /^(?!node:|bun:)/, neverBundle: [/^node:/, /^bun:/] },
		// Module path comments would embed the random staging directory in the
		// artifact and change its digest on every otherwise identical build.
		inputOptions: { experimental: { attachDebugInfo: 'none' } },
		outputOptions: { codeSplitting: false, banner: '#!/usr/bin/env node' },
		logLevel: 'silent',
	});
	const inline = await readFile(embeddedFile, 'utf8');
	if (Buffer.byteLength(inline) > 512 * 1024) {
		throw new Error('bundled provider exceeds the host 512 KiB inline program limit');
	}
	return {
		entry: provider.runtime ?? 'node',
		args: [...(provider.runtime === 'deno' ? ['run'] : []), ...(provider.runtimeArgs ?? []), '{program-file}'],
		program: { filename: 'actionlint.mjs', inline },
		stdin: 'request',
		config: { render: 'json-stdin' },
		output: {
			format: 'script-diagnostics@1',
			coordinates: { rows: 'lf', columns: 'chars', end: 'exclusive' },
		},
		...(provider.env_passthrough ? { env_passthrough: provider.env_passthrough } : {}),
	};
}

function isDocumentProvider(
	provider: PluginProvider,
): provider is DocumentProvider | DocumentProcessProvider {
	return provider.kind === 'document' || (provider.kind === 'process' && provider.role === 'document');
}

function selectorsFor(provider: PluginProvider): Record<string, JSONValue> {
	if (isDocumentProvider(provider)) {
		return { documents: provider.documentKinds ?? ['workflow', 'action'] };
	}
	return { shells: provider.shells, ...(provider.filename ? { filename: provider.filename } : {}) };
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

function isPluginProvider(value: unknown): value is PluginProvider {
	if (!isRecord(value) || (value.kind !== 'script' && value.kind !== 'process' && value.kind !== 'document')) {
		return false;
	}
	if (
		typeof value.id !== 'string' || typeof value.version !== 'string' || typeof value.name !== 'string'
		|| typeof value.description !== 'string' || typeof value.license !== 'string'
		|| typeof value.repository !== 'string' || !isRuleDescriptor(value.rule)
		|| (value.env_passthrough !== undefined && !isStringArray(value.env_passthrough))
	) return false;
	if (value.kind === 'document') {
		return typeof value.check === 'function' && isOptionalDocumentKinds(value.documentKinds)
			&& isOptionalDocumentRequirements(value.requiredInputs);
	}
	if (value.kind === 'script') {
		return typeof value.check === 'function' && isStringArray(value.shells);
	}
	const process = typeof value.entry === 'string' && Array.isArray(value.args ?? [])
		&& (value.stdin === 'request' || value.stdin === 'source') && isRecord(value.config)
		&& typeof value.config.render === 'string' && isRecord(value.output) && typeof value.output.format === 'string';
	if (!process) return false;
	if (value.role === 'document') {
		return isOptionalDocumentKinds(value.documentKinds) && isOptionalDocumentRequirements(value.requiredInputs);
	}
	return (value.role === undefined || value.role === 'script') && isStringArray(value.shells);
}

function isStringArray(value: unknown): value is string[] {
	return Array.isArray(value) && value.every((item) => typeof item === 'string');
}

function isOptionalDocumentKinds(value: unknown): value is DocumentKind[] | undefined {
	return value === undefined
		|| (Array.isArray(value) && value.every((kind) => kind === 'workflow' || kind === 'action'));
}

function isOptionalDocumentRequirements(value: unknown): value is DocumentRequirement[] | undefined {
	return value === undefined
		|| (Array.isArray(value)
			&& value.every((requirement) => requirement === 'source' || requirement === 'dependencies'));
}

function isRuleDescriptor(value: unknown): value is RuleDescriptor {
	return isRecord(value) && typeof value.name === 'string' && isRuleCategory(value.category)
		&& typeof value.subcategory === 'string' && (value.maturity === 'stable' || value.maturity === 'nursery')
		&& typeof value.description === 'string' && typeof value.provider === 'string';
}

function isRecord(value: unknown): value is Record<string, JSONValue> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}
