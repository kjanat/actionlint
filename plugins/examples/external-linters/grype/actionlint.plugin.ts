import { access } from 'node:fs/promises';
import { isAbsolute, resolve } from 'node:path';

import type { DocumentDiagnostic, Severity } from '@kjanat-actionlint/plugin-sdk';
import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { configObject } from '@kjanat-actionlint/plugin-sdk/config';
import { diagnosticAtImage, imageKey, imageReferences } from '@kjanat-actionlint/plugin-sdk/images';
import { array, record, runCommand, text } from '@kjanat-actionlint/plugin-sdk/process';

export function parseGrypeJSON(
	output: string,
	filename: string,
	image: ReturnType<typeof imageReferences>[number],
): DocumentDiagnostic[] {
	let value: unknown;
	try {
		value = JSON.parse(output);
	} catch (error) {
		throw new Error(`Grype returned invalid JSON: ${errorMessage(error)}`);
	}
	const document = record(value, 'Grype output');
	const matches = array(document.matches, 'Grype matches');
	return matches.map((value) => {
		const match = record(value, 'Grype match');
		const vulnerability = record(match.vulnerability, 'Grype vulnerability');
		const artifact = record(match.artifact, 'Grype package');
		const severity = grypeSeverity(vulnerability.severity);
		const id = text(vulnerability.id, 'Grype vulnerability id');
		const name = text(artifact.name, 'Grype package name');
		const version = text(artifact.version, 'Grype package version');
		return diagnosticAtImage(
			filename,
			image,
			id,
			`${id} (${severity.toUpperCase()}) affects ${name}@${version} in ${image.value}`,
			severity,
		);
	});
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/grype',
	version: '0.1.0',
	name: 'Grype image vulnerability provider',
	description: 'Reports Grype vulnerability matches for static container references in the captured document.',
	license: 'Apache-2.0',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow', 'action'],
	requiredInputs: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	resources: ['directory'],
	rule: {
		name: 'grype',
		category: 'security',
		subcategory: 'container',
		maturity: 'stable',
		description: 'Report vulnerability matches for declared container images.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/grype',
	},
	check: async (request) => {
		const references = imageReferences(request.source, request.kind);
		if (references.length === 0) return [];
		const config = configObject(request.config);
		const sbomDirectoryValue = config.sbom_dir;
		if (
			sbomDirectoryValue !== undefined
			&& (typeof sbomDirectoryValue !== 'string' || sbomDirectoryValue.trim().length === 0)
		) {
			throw new Error('plugin config sbom_dir must be a nonempty path');
		}
		const base = request.directory ?? process.cwd();
		const sbomDirectory = typeof sbomDirectoryValue === 'string'
			? isAbsolute(sbomDirectoryValue) ? sbomDirectoryValue : resolve(base, sbomDirectoryValue)
			: undefined;
		const diagnostics: DocumentDiagnostic[] = [];
		for (const reference of references) {
			let target = `registry:${reference.value}`;
			if (sbomDirectory !== undefined) {
				target = resolve(sbomDirectory, `${imageKey(reference.value)}.syft.json`);
				try {
					await access(target);
				} catch {
					throw new Error(`No Syft SBOM exists for ${reference.value} at ${target}`);
				}
			}
			const result = await runCommand('grype', [target, '--quiet', '--output', 'json']);
			if (result.stdout.trim().length === 0) throw new Error(`Grype produced no JSON for ${reference.value}`);
			diagnostics.push(...parseGrypeJSON(result.stdout, request.filename, reference));
		}
		return diagnostics;
	},
});

function grypeSeverity(value: unknown): Severity {
	if (typeof value !== 'string') throw new Error('Grype severity must be a string');
	switch (value.toLowerCase()) {
		case 'critical':
		case 'high':
			return 'error';
		case 'medium':
			return 'warning';
		case 'low':
		case 'negligible':
		case 'unknown':
			return 'info';
		default:
			throw new Error(`Grype returned unsupported severity ${value}`);
	}
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
