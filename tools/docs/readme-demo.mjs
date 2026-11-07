import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, resolve } from 'node:path';
import { parseArgs } from 'node:util';

const begin = '<!-- BEGIN generated demo -->';
const end = '<!-- END generated demo -->';
const fixture = 'docs/screenshots';
const workflow = 'demo-workflow.yaml';
const fork = 'github:kjanat/actionlint';
const upstream = 'github:rhysd/actionlint';

/** Count each reported rule in the order it first appears. @param {string} output */
export function summarize(output) {
	/** @type {Map<string, number>} */
	const counts = new Map();
	for (const line of output.split(/\r?\n/)) {
		const match = /\[([a-z0-9-]+)\]$/.exec(line);
		if (match) counts.set(match[1], (counts.get(match[1]) ?? 0) + 1);
	}
	return [...counts].map(([name, count]) => `\`${name}\`${count > 1 ? ` ×${count}` : ''}`).join(', ');
}

/** @param {string} output */
export function normalizeDiagnostics(output) {
	return output.replaceAll('\r\n', '\n').replaceAll(`${fixture}/`, '').replaceAll('docs\\screenshots\\', '');
}

/** @param {string} document @param {string} section */
export function replaceSection(document, section) {
	const start = document.indexOf(begin);
	const finish = document.indexOf(end);
	if (start < 0) throw new Error(`The document has no ${begin} marker.`);
	if (finish < 0) throw new Error(`The document has no ${end} marker.`);
	if (finish < start) throw new Error(`The ${end} marker comes before ${begin}.`);
	return document.slice(0, start) + section + document.slice(finish + end.length);
}

/** @param {string} root @param {string} cwd @param {string[]} args @param {boolean} allowFindings */
function command(root, cwd, args, allowFindings = false) {
	try {
		return execFileSync('mise', args, {
			cwd,
			encoding: 'utf8',
			windowsHide: true,
			stdio: ['ignore', 'pipe', 'pipe'],
			env: {
				...process.env,
				MISE_YES: '1',
				MISE_SILENT: '1',
				MISE_SAFE: '1',
				MISE_ENABLE_TOOLS: `${fork},${upstream}`,
				MISE_TRUSTED_CONFIG_PATHS: root,
				MISE_MINIMUM_RELEASE_AGE_EXCLUDES: fork,
			},
		});
	} catch (error) {
		if (error instanceof Error && 'status' in error && 'stderr' in error && 'stdout' in error) {
			const output = String(error.stdout);
			if (allowFindings && error.status === 1 && /^.+:\d+:\d+: /m.test(output)) {
				if (String(error.stderr).length !== 0) process.stderr.write(String(error.stderr));
				return output;
			}
			throw new Error(`Could not run mise ${args.join(' ')}: ${String(error.stderr).trim()}`, { cause: error });
		}
		throw error;
	}
}

/** @param {string} root @param {string} tool */
function releasedBinary(root, tool) {
	const directory = mkdtempSync(join(tmpdir(), 'actionlint-readme-release-'));
	try {
		command(root, directory, ['install', `${tool}@latest`]);
		const binary = command(root, directory, ['which', 'actionlint', '--tool', `${tool}@latest`]).trim();
		if (!isAbsolute(binary) || /[\r\n]/.test(binary)) {
			throw new Error(`Mise returned an invalid executable path for ${tool}.`);
		}
		return binary;
	} finally {
		rmSync(directory, { recursive: true, force: true });
	}
}

/** @param {string} root @param {string} tool @param {boolean} configuration @param {(message: string) => void} log */
function measure(root, tool, configuration, log) {
	const binary = releasedBinary(root, tool);
	/** @param {string[]} args */
	const run = (args, findings = false) =>
		command(root, root, ['exec', '--no-deps', `${tool}@latest`, '--', binary, ...args], findings);
	const firstLine = run(['-version']).trim().split(/\r?\n/)[0];
	const version = configuration ? firstLine.slice(firstLine.lastIndexOf(' ') + 1) : firstLine;
	if (!version) throw new Error(`Mise reported no version for ${tool}.`);
	log(`Checking ${workflow} with ${version}`);
	const args = ['-no-color', '-shellcheck='];
	if (configuration) args.push('-config-file', `${fixture}/actionlint.yaml`);
	args.push(`${fixture}/${workflow}`);
	const output = normalizeDiagnostics(run(args, true));
	if (configuration && !output) throw new Error(`${workflow} produced no findings for the demonstration.`);
	return { version, output };
}

/** @param {string[]} args */
export function main(args) {
	const { values, positionals } = parseArgs({
		args,
		allowPositionals: true,
		options: { fix: { type: 'boolean' }, quiet: { type: 'boolean' }, help: { type: 'boolean', short: 'h' } },
	});
	if (values.help) {
		console.log(
			'Usage: node tools/docs/readme-demo.mjs [--fix] [--quiet] FILE\n\nCompare released actionlint versions and update the marked README section.\n\nExample: node tools/docs/readme-demo.mjs --fix README.md',
		);
		return;
	}
	if (positionals.length !== 1) throw new Error('Specify one Markdown file.');
	const path = resolve(positionals[0]);
	const root = dirname(path);
	const document = readFileSync(path, 'utf8');
	replaceSection(document, '');
	/** @param {string} message */
	const log = message => {
		if (!values.quiet) console.error(message);
	};
	/** @param {string} name */
	const read = name => readFileSync(join(root, fixture, name), 'utf8').replace(/\n+$/, '');
	const source = read(workflow);
	const configuration = read('actionlint.yaml');
	const current = measure(root, fork, true, log);
	const original = measure(root, upstream, false, log);
	/** @param {string} label @param {{version: string, output: string}} result */
	const block = (label, result) =>
		`**${label} ${result.version} reports ${[...result.output.matchAll(/^demo-workflow\.yaml:\d+:\d+:/gm)].length}: ${
			summarize(result.output)
		}**\n\n\`\`\`console\n${result.output}\`\`\`\n\n`;
	const section =
		`${begin}\n\n\`${fixture}/${workflow}\`:\n\n\`\`\`yaml\n${source}\n\`\`\`\n\n\`${fixture}/actionlint.yaml\`:\n\n\`\`\`yaml\n${configuration}\n\`\`\`\n\n${
			block('Upstream actionlint', original)
		}${block('This fork', current)}${end}`;
	const updated = replaceSection(document, section);
	if (updated === document) return log('The demonstration is up to date.');
	if (!values.fix) {
		throw new Error(`The demonstration is outdated. Run node tools/docs/readme-demo.mjs --fix ${positionals[0]}.`);
	}
	writeFileSync(path, updated);
	log(`Updated ${path}`);
}

if (process.argv[1] && resolve(process.argv[1]) === import.meta.filename) {
	try {
		main(process.argv.slice(2));
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
