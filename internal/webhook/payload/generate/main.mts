import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { convertOpenAPI, object } from './openapi.mts';

const pin = object(JSON.parse(readFileSync(new URL('./source.json', import.meta.url), 'utf8')));
if (typeof pin.url !== 'string' || typeof pin.sha256 !== 'string' || typeof pin.revision !== 'string') {
	throw new Error('Invalid pinned OpenAPI provenance');
}
const file = process.argv[2];
let bytes: Uint8Array;
if (file) {
	bytes = readFileSync(file);
} else {
	const response = await fetch(pin.url, { signal: AbortSignal.timeout(30_000) });
	if (!response.ok) throw new Error(`OpenAPI download failed: ${response.status}`);
	bytes = new Uint8Array(await response.arrayBuffer());
}
const sha256 = createHash('sha256').update(bytes).digest('hex');
if (sha256 !== pin.sha256) throw new Error(`OpenAPI digest mismatch: ${sha256}`);
const { events, nodes } = convertOpenAPI(JSON.parse(new TextDecoder().decode(bytes)));
const catalogue = {
	source: { package: '@octokit/openapi-webhooks', version: pin.revision, sha256, url: pin.url },
	events,
	nodes,
};
const formatted = execFileSync('dprint', ['fmt', '--stdin', 'catalogue.json'], {
	input: JSON.stringify(catalogue),
	maxBuffer: 32 << 20,
});
writeFileSync(resolve(import.meta.dirname, '../catalogue.json'), formatted);
console.log(`Generated ${Object.keys(events).length} events and ${nodes.length} shapes from ${pin.revision}`);
