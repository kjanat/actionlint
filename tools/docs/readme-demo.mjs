import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { parseArgs } from 'node:util';

const begin = '<!-- BEGIN generated demo -->';
const end = '<!-- END generated demo -->';

/** @param {string} root @param {string} source */
function checkExample(root, source) {
	const temporary = mkdtempSync(join(tmpdir(), 'actionlint-readme-'));
	const binary = join(temporary, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');
	try {
		execFileSync('go', ['build', '-buildvcs=false', '-o', binary, './cmd/actionlint'], {
			cwd: root,
			stdio: ['ignore', 'pipe', 'pipe'],
			windowsHide: true,
		});
		try {
			return execFileSync(binary, [
				'check',
				'--no-config',
				'--color=never',
				'--output-format=pretty',
				'--diagnostic-width=90',
				'--hyperlinks=never',
				'--quiet',
				'--offline',
				'--no-tool=shellcheck',
				'--no-tool=ruff',
				'--stdin-filename=.github/workflows/ci.yml',
				'-',
			], { cwd: root, input: source, encoding: 'utf8', windowsHide: true });
		} catch (error) {
			if (error instanceof Error && 'status' in error && error.status === 1 && 'stdout' in error) {
				return String(error.stdout);
			}
			throw error;
		}
	} finally {
		rmSync(temporary, { recursive: true, force: true });
	}
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
			'Usage: node tools/docs/readme-demo.mjs [--fix] [--quiet] FILE\n\nBuild actionlint and refresh the diagnostics for the marked YAML example.',
		);
		return;
	}
	if (positionals.length !== 1) throw new Error('Specify one Markdown file.');
	const path = resolve(positionals[0]);
	const document = readFileSync(path, 'utf8').replaceAll('\r\n', '\n');
	const start = document.indexOf(begin);
	const finish = document.indexOf(end, start + begin.length);
	if (start < 0 || finish < start) throw new Error('The README has no marked example.');
	const source = /```yaml\n([\s\S]*?)\n```/.exec(document.slice(start, finish))?.[1];
	if (!source) throw new Error('The marked example has no YAML code block.');
	const output = checkExample(dirname(path), `${source}\n`).replaceAll('\r\n', '\n').trimEnd();
	if (!output) throw new Error('The example produced no diagnostics.');
	const section = `${begin}\n\n\`\`\`yaml\n${source}\n\`\`\`\n\n\`\`\`console\n${output}\n\`\`\`\n\n${end}`;
	const updated = document.slice(0, start) + section + document.slice(finish + end.length);
	if (updated === document) {
		if (!values.quiet) console.error('The README example is up to date.');
		return;
	}
	if (!values.fix) {
		throw new Error(`Run node tools/docs/readme-demo.mjs --fix ${positionals[0]} to update the example.`);
	}
	writeFileSync(path, updated);
	if (!values.quiet) console.error(`Updated ${path}`);
}

if (process.argv[1] && resolve(process.argv[1]) === import.meta.filename) {
	try {
		main(process.argv.slice(2));
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
