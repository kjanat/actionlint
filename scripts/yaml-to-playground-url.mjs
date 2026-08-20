#!/usr/bin/env node

// This script inputs YAML workflow code from stdin and outputs a playground URL
// for the workflow to stdout.
//
// Usage:
//   pbpaste | node ./scripts/yaml-to-playground-url.mjs
//   node ./scripts/yaml-to-playground-url.mjs < test.yaml

import { readFileSync } from 'node:fs';
import { deflate } from 'pako';

const re = /^\s*#/;
const stdin = readFileSync(process.stdin.fd, 'utf8').trim();
const lines = stdin.split('\n').filter(l => !re.test(l)); // remove comment lines
const src = lines.join('\n');
const compressed = deflate(new TextEncoder().encode(src));
const b64 = Buffer.from(compressed).toString('base64');
console.log(`https://kjanat.github.io/actionlint#${b64}`);
