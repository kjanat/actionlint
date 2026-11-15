// Package workflowvisit defines the synchronous workflow visitor contract, the
// invocation participant contracts, and the driver that traverses workflow
// syntax with caller-supplied passes. Document decoding, composition, execution
// and diagnostic policy have separate owners; rule adapters and the executor
// both depend on this shared contract.
package workflowvisit
