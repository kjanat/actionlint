import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { expect, test } from 'bun:test';

const executable = process.env.ACTIONLINT;
const enabled = process.env.ACTIONLINT_REGISTRY_TEST === '1';
const local = process.env.ACTIONLINT_PLUGIN_TEST_MODE === 'local';
const image = 'docker.io/library/busybox:1.37.0';
const examples = resolve(import.meta.dirname, '../..');
const workflow = `on: pull_request_target
permissions: write-all
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: \${{ github.event.pull_request.head.sha }}
      - shell: bash
        run: |
          echo example  
          echo "\${{ github.event.pull_request.title }}"
`;
const imageWorkflow = `on: push
jobs:
  lint:
    runs-on: ubuntu-latest
    container: ${image}
    steps:
      - run: echo example
`;

function object(value: unknown): Record<string, unknown> {
	if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Expected an object');
	return Object.fromEntries(Object.entries(value));
}

const cases = [
	{
		name: 'trailing-space',
		directory: 'typescript',
		category: 'conventions',
		source: workflow,
		options: {},
		outcome: 'diagnostic',
	},
	{
		name: 'yamllint',
		directory: 'external-linters/yamllint',
		category: 'conventions',
		source: workflow,
		options: { config_data: '{rules: {trailing-spaces: enable}}' },
		outcome: 'diagnostic',
	},
	{
		name: 'zizmor',
		directory: 'external-linters/zizmor',
		category: 'security',
		source: workflow,
		options: {},
		outcome: 'diagnostic',
	},
	{
		name: 'poutine',
		directory: 'external-linters/poutine',
		category: 'security',
		source: workflow,
		options: {},
		outcome: 'diagnostic',
	},
	{
		name: 'runner-guard',
		directory: 'external-linters/runner-guard',
		category: 'security',
		source: workflow,
		options: {},
		outcome: 'diagnostic',
	},
	{
		name: 'syft',
		directory: 'external-linters/syft',
		category: 'security',
		source: imageWorkflow,
		options: { output_dir: 'sbom' },
		outcome: 'sbom',
	},
	{
		name: 'grype',
		directory: 'external-linters/grype',
		category: 'security',
		source: imageWorkflow,
		options: { sbom_dir: 'sbom' },
		outcome: 'clean',
	},
	{
		name: 'grant',
		directory: 'external-linters/grant',
		category: 'security',
		source: imageWorkflow,
		options: { sbom_dir: 'sbom', policy_file: 'grant.yaml' },
		outcome: 'diagnostic',
	},
	{
		name: 'psscriptanalyzer',
		directory: 'psscriptanalyzer',
		category: 'correctness',
		source:
			'on: push\njobs:\n  lint:\n    runs-on: windows-latest\n    steps:\n      - shell: pwsh\n        run: gci .\n',
		options: { IncludeRules: ['PSAvoidUsingCmdletAliases'] },
		outcome: 'diagnostic',
	},
];

for (const entry of cases) {
	test.skipIf(!enabled)(`${entry.name} installs and executes with the real scanner`, async () => {
		if (!executable) throw new Error('Set ACTIONLINT to the actionlint executable');
		const directory = await mkdtemp(join(tmpdir(), `actionlint-${entry.name}-`));
		try {
			await mkdir(join(directory, '.git'));
			await mkdir(join(directory, '.github'));
			await mkdir(join(directory, 'sbom'));
			await writeFile(join(directory, 'workflow.yml'), entry.source);
			await writeFile(
				join(directory, 'grant.yaml'),
				'allow: []\nrequire-license: true\nrequire-known-license: false\n',
			);
			const key = createHash('sha256').update(image).digest('hex');
			await copyFile(
				join(examples, 'external-linters/testdata/syft.sbom.json'),
				join(directory, `sbom/${key}.syft.json`),
			);
			const config = {
				tools: { shellcheck: false, ruff: false },
				lint: {
					rules: { preset: 'none', [entry.category]: { [entry.name]: { level: 'warn', options: entry.options } } },
				},
			};
			await writeFile(join(directory, '.github/actionlint.json'), JSON.stringify(config));
			const env = { ...process.env, XDG_CACHE_HOME: join(directory, 'cache'), LOCALAPPDATA: join(directory, 'cache') };
			const run = async (args: string[], offline = false) => {
				const processEnv = offline
					? {
						...env,
						HTTP_PROXY: 'http://127.0.0.1:9',
						HTTPS_PROXY: 'http://127.0.0.1:9',
						ALL_PROXY: 'http://127.0.0.1:9',
						NO_PROXY: '',
					}
					: env;
				const child = Bun.spawn([resolve(executable), ...args], {
					cwd: directory,
					env: processEnv,
					stdout: 'pipe',
					stderr: 'pipe',
				});
				const [stdout, stderr, code] = await Promise.all([
					new Response(child.stdout).text(),
					new Response(child.stderr).text(),
					child.exited,
				]);
				expect(code, stderr || stdout).toBeLessThan(2);
				return stdout;
			};
			if (!local) await run(['plugin', 'install', entry.name]);
			const args = ['check', '--allow-config-processes', '--json', 'workflow.yml'];
			if (local) args.push('--plugin', join(examples, entry.directory, 'actionlint-provider.json'));
			const verify = async (args: string[], offline = false) => {
				const result = object(JSON.parse(await run(args, offline)));
				expect(result.completed).toBe(true);
				if (!Array.isArray(result.diagnostics)) throw new Error('Missing diagnostics');
				const findings = result.diagnostics.map(object).filter(diagnostic => diagnostic.rule === entry.name);
				if (entry.outcome === 'diagnostic') {
					expect(findings.length).toBeGreaterThan(0);
					for (const finding of findings) {
						expect(finding.path).toBe('workflow.yml');
						expect(object(finding.start).line).toBeGreaterThan(0);
					}
				} else expect(findings).toEqual([]);
				if (entry.outcome === 'sbom') {
					const files = await readdir(join(directory, 'sbom'));
					expect(files).toContain(`${key}.syft.json`);
					const sbom = object(JSON.parse(await readFile(join(directory, `sbom/${key}.syft.json`), 'utf8')));
					expect(object(sbom.descriptor).name).toBe('syft');
				}
			};
			if (entry.outcome === 'sbom') await rm(join(directory, `sbom/${key}.syft.json`));
			await verify(args);
			if (!local) await verify(args, true);
		} finally {
			await rm(directory, { recursive: true, force: true });
		}
	}, 240_000);
}
