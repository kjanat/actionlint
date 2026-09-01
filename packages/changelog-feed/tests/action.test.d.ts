import type { AsyncFunctionArguments } from '@actions/github-script';

import run from '#action';

declare const args: AsyncFunctionArguments;
run(args);
