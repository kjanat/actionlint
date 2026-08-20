import { promises as fs } from 'node:fs';
import path from 'node:path';
import object from './object.mjs';

async function main(args) {
	const json = JSON.stringify(object, null, 2);
	if (args.length === 0) {
		console.log(json);
	} else {
		const target = args[0];
		const root = process.cwd();
		const dest = path.resolve(target);
		if (!dest.startsWith(root + path.sep)) {
			throw new Error(`output file path must stay within ${root}: ${target}`);
		}
		await fs.writeFile(dest, json + '\n', 'utf8');
		console.log(`Wrote to ${target}`);
	}
}

main(process.argv.slice(2));
