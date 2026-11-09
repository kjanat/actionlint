import { createHash } from 'node:crypto';

import type { ImageReference } from '#images';
import type { DocumentDiagnostic, Severity } from '#index';

/** Stable SHA-256 key for associating an image reference with cached external evidence. */
export function imageKey(reference: string): string {
	return createHash('sha256').update(reference).digest('hex');
}

/** Build a document finding whose range covers the complete extracted image reference. */
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
