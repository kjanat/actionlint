// Package builtin composes built-in diagnostic adapters from resolved inputs and
// catalogue metadata. Workflow and action domains own the checks. The traversal
// engine depends on this composition boundary; adapters never import the engine,
// application sessions, native tools or output rendering.
package builtin
