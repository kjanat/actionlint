import { access, mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { isAbsolute, join, resolve } from 'node:path';

import type { DocumentDiagnostic } from '@kjanat-actionlint/plugin-sdk';
import { defineDocumentProvider } from '@kjanat-actionlint/plugin-sdk';
import { configObject } from '@kjanat-actionlint/plugin-sdk/config';
import { diagnosticAtImage, imageKey, imageReferences } from '@kjanat-actionlint/plugin-sdk/images';
import { array, record, runCommand, text } from '@kjanat-actionlint/plugin-sdk/process';

export function parseGrantJSON(
	output: string,
	filename: string,
	image: ReturnType<typeof imageReferences>[number],
): DocumentDiagnostic[] {
	let value: unknown;
	try {
		value = JSON.parse(output);
	} catch (error) {
		throw new Error(`Grant returned invalid JSON: ${errorMessage(error)}`);
	}
	const document = record(value, 'Grant output');
	if (document.tool !== 'grant') throw new Error('Grant JSON did not identify its tool');
	const run = record(document.run, 'Grant run');
	const targets = array(run.targets, 'Grant targets');
	if (targets.length !== 1 || targets[0] === undefined) {
		throw new Error(`Grant evaluated ${targets.length} targets; expected one SBOM`);
	}
	const target = record(targets[0], 'Grant target');
	const evaluation = record(target.evaluation, 'Grant evaluation');
	const status = text(evaluation.status, 'Grant evaluation status');
	if (status === 'error') throw new Error(`Grant could not evaluate the SBOM for ${image.value}`);
	if (status !== 'compliant' && status !== 'noncompliant') {
		throw new Error(`Grant returned unsupported evaluation status ${status}`);
	}
	const findings = record(evaluation.findings, 'Grant findings');
	const packages = array(findings.packages, 'Grant package findings');
	const diagnostics: DocumentDiagnostic[] = [];
	for (const packageValue of packages) {
		const item = record(packageValue, 'Grant package finding');
		const decision = text(item.decision, 'Grant package decision');
		if (decision !== 'deny') continue;
		const name = text(item.name, 'Grant package name');
		const version = text(item.version, 'Grant package version');
		const licenses = array(item.licenses, 'Grant package licenses').map((licenseValue) => {
			const license = record(licenseValue, 'Grant license');
			return typeof license.id === 'string' && license.id.length > 0
				? license.id
				: typeof license.name === 'string' && license.name.length > 0
				? license.name
				: undefined;
		}).filter((license): license is string => license !== undefined);
		const reason = licenses.length > 0
			? `denied licenses: ${licenses.join(', ')}`
			: 'no detected license is allowed by policy';
		diagnostics.push(diagnosticAtImage(
			filename,
			image,
			'grant/license-policy',
			`Grant denied ${name}@${version} in ${image.value}: ${reason}`,
			'error',
		));
	}
	if (status === 'noncompliant' && diagnostics.length === 0) {
		throw new Error(`Grant reported a noncompliant image without denied package findings: ${image.value}`);
	}
	return diagnostics;
}

export default defineDocumentProvider({
	id: 'github.com/kjanat/actionlint/examples/external-linters/grant',
	version: '0.1.0',
	name: 'Grant license policy provider',
	description: 'Checks Syft SBOM files against a Grant policy for images in the captured document.',
	license: 'Apache-2.0',
	repository: 'https://github.com/kjanat/actionlint',
	documentKinds: ['workflow', 'action'],
	requires: ['source'],
	env_passthrough: ['PATH', 'SystemRoot', 'COMSPEC', 'PATHEXT', 'TEMP', 'TMP', 'HOME', 'USERPROFILE'],
	resources: ['directory'],
	rule: {
		name: 'grant',
		category: 'security',
		subcategory: 'container-licenses',
		maturity: 'stable',
		description: 'Report denied package licenses from the configured Grant policy.',
		provider: 'github.com/kjanat/actionlint/examples/external-linters/grant',
	},
	check: async (request) => {
		const references = imageReferences(request.source, request.kind);
		if (references.length === 0) return [];
		const config = configObject(request.config);
		const sbomDirectoryValue = config.sbom_dir;
		const policyValue = config.policy_file;
		if (typeof sbomDirectoryValue !== 'string' || sbomDirectoryValue.trim().length === 0) {
			throw new Error('plugin config sbom_dir must name the Syft SBOM directory');
		}
		if (typeof policyValue !== 'string' || policyValue.trim().length === 0) {
			throw new Error('plugin config policy_file must name the Grant license policy');
		}
		const base = request.directory ?? process.cwd();
		const sbomDirectory = isAbsolute(sbomDirectoryValue) ? sbomDirectoryValue : resolve(base, sbomDirectoryValue);
		const policyFile = isAbsolute(policyValue) ? policyValue : resolve(base, policyValue);
		const diagnostics: DocumentDiagnostic[] = [];
		for (const image of references) {
			const sbom = resolve(sbomDirectory, `${imageKey(image.value)}.syft.json`);
			try {
				await access(sbom);
			} catch {
				throw new Error(`No Syft SBOM exists for ${image.value} at ${sbom}`);
			}
			const directory = await mkdtemp(join(tmpdir(), 'actionlint-grant-'));
			const report = join(directory, 'grant.json');
			try {
				await runCommand('grant', /* dprint-ignore */ [
					'check',
					'--output', 'json',
					'--output-file', report,
					'--no-output',
					'--dry-run',
					'--config', policyFile,
					sbom,
				]);
				diagnostics.push(...parseGrantJSON(await readFile(report, 'utf8'), request.filename, image));
			} finally {
				await rm(directory, { recursive: true, force: true });
			}
		}
		return diagnostics;
	},
});

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
