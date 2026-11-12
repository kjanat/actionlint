#!/usr/bin/env node

// Read a YAML workflow from stdin and write its playground URL to stdout.
// pbpaste | node ./applications/playground/tooling/yaml-to-playground-url.mjs
// node ./applications/playground/tooling/yaml-to-playground-url.mjs < test.yaml

import { Buffer } from 'node:buffer';
import console from 'node:console';
import process from 'node:process';

import { deflate } from 'pako';

// applications/playground/src/source.ts refuses shared sources past this size.
const maxSourceBytes = 16 * 1024 * 1024;
// Chromium refuses URLs longer than 2 MiB.
const maxURLLength = 2 * 1024 * 1024;

const chunks = [];
for await (const chunk of process.stdin) {
	chunks.push(chunk);
}
const src = Buffer.concat(chunks);
if (src.length > maxSourceBytes) {
	console.error(`The source is larger than ${maxSourceBytes} bytes, which the playground refuses to load`);
	process.exit(1);
}
const compressed = deflate(src);
const url = `https://kjanat.github.io/actionlint#${Buffer.from(compressed).toString('base64')}`;
if (url.length > maxURLLength) {
	console.error(`The playground URL is ${url.length} characters, longer than the ${maxURLLength} browsers load`);
	process.exit(1);
}
console.log(url);
