import { createHash } from 'node:crypto';

import type { ImageReference } from '#images';
import type { DocumentDiagnostic, Severity } from '#index';

/** Return a stable SHA-256 key for an image reference. */
export function imageKey(reference: string): string {
	return createHash('sha256').update(reference).digest('hex');
}

export function diagnosticAtImage(
	filename: string,
	image: ImageReference,
	code: string,
	message: string,
	severity: Severity,
): DocumentDiagnostic {
	return {
		filename,
		code,
		message,
		severity,
		location: image.start,
		end_location: image.end,
	};
}
