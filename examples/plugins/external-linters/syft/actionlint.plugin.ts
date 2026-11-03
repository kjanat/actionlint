import { mkdir, readFile } from 'node:fs/promises';
import { isAbsolute, join, resolve } from 'node:path';

import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { configObject } from '@kjanat-actionlint/plugin-sdk/config';
import { imageKey, imageReferences } from '@kjanat-actionlint/plugin-sdk/images';
import { record, runCommand } from '@kjanat-actionlint/plugin-sdk/process';

export function validateSyftSBOM(output: string): void {
	let value: unknown;
	try {
		value = JSON.parse(output);
	} catch (error) {
		throw new Error(`Syft returned invalid JSON: ${errorMessage(error)}`);
	}
	const sbom = record(value, 'Syft SBOM');
	if (!Array.isArray(sbom.artifacts) || typeof sbom.schema !== 'object' || sbom.schema === null) {
		throw new Error('Syft output is not a complete Syft JSON SBOM');
	}
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/syft',
	version: '0.1.0',
	name: 'Syft image inventory provider',
	description: 'Writes Syft JSON SBOM files for static container references in the captured workflow or action.',
	license: 'Apache-2.0',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow', 'action'],
	requires: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	resources: ['directory'],
	rule: {
		name: 'syft',
		category: 'security',
		subcategory: 'container',
		maturity: 'stable',
		description: 'Generate SBOM artifacts for container images declared in the captured document.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/syft',
	},
	check: async (request) => {
		const references = imageReferences(request.source, request.kind);
		if (references.length === 0) return [];
		const config = configObject(request.config);
		const outputDirectoryValue = config.output_dir;
		if (typeof outputDirectoryValue !== 'string' || outputDirectoryValue.trim().length === 0) {
			throw new Error('plugin config output_dir must name where Syft writes SBOM files');
		}
		const base = request.directory ?? process.cwd();
		const outputDirectory = isAbsolute(outputDirectoryValue)
			? outputDirectoryValue
			: resolve(base, outputDirectoryValue);
		await mkdir(outputDirectory, { recursive: true });
		const seen = new Set<string>();
		for (const reference of references) {
			if (seen.has(reference.value)) continue;
			seen.add(reference.value);
			const output = join(outputDirectory, `${imageKey(reference.value)}.syft.json`);
			await runCommand('syft', /* dprint-ignore */ [
				'scan', `registry:${reference.value}`,
				'--quiet',
				'--output', `syft-json=${output}`,
			]);
			try {
				validateSyftSBOM(await readFile(output, 'utf8'));
			} catch (error) {
				throw new Error(`Syft output for ${reference.value} is invalid: ${errorMessage(error)}`);
			}
		}
		return [];
	},
});

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
