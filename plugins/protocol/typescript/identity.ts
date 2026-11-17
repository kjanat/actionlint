import * as Exported from './.generated/identity/interfaces/exports/test-contracts-documents.js';
import type * as Imported from './.generated/identity/interfaces/test-contracts-documents.js';

const hostDocument: Imported.Document = { read: () => 'text' };
const guestDocument: Exported.Document = new Exported.Document('text');
const text: string = guestDocument.read();
const failure: Exported.FailureError = new Exported.FailureError({ message: 'missing' });
const message: string = failure.payload.message;
void [hostDocument, text, message];
// @ts-expect-error host mocks do not carry the exported resource identity.
const invalid: Exported.Document = hostDocument;
// @ts-expect-error named errors preserve the WIT payload type.
new Exported.FailureError({ message: 123 });
void invalid;
