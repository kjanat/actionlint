#!/usr/bin/env node

// This script inputs YAML workflow code from stdin and outputs a playground URL
// for the workflow to stdout.
//
// Usage:
//   pbpaste | node ./scripts/yaml-to-playground-url.mjs
//   node ./scripts/yaml-to-playground-url.mjs < test.yaml

import { deflate } from 'pako';

// playground/src/source.ts refuses shared sources past this size.
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
