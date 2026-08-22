import { reconcileTrackingIssue } from './shellcheck-version-lifecycle.mjs';

/**
 * Scheduled tracking-issue entry point.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function run({ github, context, core }) {
	await reconcileTrackingIssue({ github, context, core });
}
