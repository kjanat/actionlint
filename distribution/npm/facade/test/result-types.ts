// Compile against the package export, as an npm consumer would.
import type { CheckResult, CheckResultV1, DiagnosticRecord } from '@kjanat/actionlint/result';

export function completedStatus(result: CheckResult): 0 | 1 | undefined {
	if (result.completed) return result.exit_code;
	return undefined;
}

export function failureStatus(result: CheckResultV1): 2 | 3 | undefined {
	if (!result.completed) return result.exit_code;
	return undefined;
}

export function recordVersion(record: DiagnosticRecord): 1 {
	return record.schema_version;
}
