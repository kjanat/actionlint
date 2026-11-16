// Package define declares a native rule once: its metadata, options,
// enablement, input preparation and the single way it executes.
//
// A [Rule] holds the facts that describe the rule and exactly one executable
// form: [Rule.Checks] for stateless checks of published workflow views,
// [Rule.Walker] for a stateful traversal, or [Rule.Script] for a script
// analyzer. A rule with none of them is declaration-only; its findings come
// from the component named by [Rule.DeclaredBy]. Declarations, traversals
// and script analyzers are derived from the rule, so no other place
// repeats its name, defaults or wiring.
package define
