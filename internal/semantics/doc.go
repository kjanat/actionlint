// Package semantics groups checks over plain values and diagnostic positions.
// Its child packages know nothing about workflow syntax, YAML decoding, repository
// configuration or analyzer lifetimes. Workflow and action adapters supply values
// to these checks; dependencies never point back from semantics to those adapters.
package semantics
