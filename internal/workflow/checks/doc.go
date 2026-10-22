// Package checks adapts workflow declarations to semantic inputs and implements
// checks that depend on workflow syntax. Language-independent checks receive plain
// values and diagnostic positions through internal/semantics. Dependencies point
// from workflow/checks to semantics, never from semantics back to workflow syntax.
// Checks have no analyzer runtime, repository configuration or visitor state.
package checks
