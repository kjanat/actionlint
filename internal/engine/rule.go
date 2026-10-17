package engine

import (
	"fmt"
	"io"
)

// ruleConfiguration is used only by adapters with configuration-dependent semantics.
type ruleConfiguration struct{ config *Config }

// ruleLogger is used only by adapters that produce trace output.
type ruleLogger struct {
	tag string
	dbg io.Writer
}

// Debug prints debug log to the output.
// The output is specified by the argument of EnableDebug method.
// By default, no output is set so debug log is not printed.
func (r *ruleLogger) Debug(format string, args ...any) {
	if r.dbg == nil {
		return
	}
	format = fmt.Sprintf("[%s] %s\n", r.tag, format)
	_, _ = fmt.Fprintf(r.dbg, format, args...)
}

// EnableDebug enables debug output from the rule.
// Given io.Writer instance is used to print debug information to console.
// Setting nil means disabling debug output.
func (r *ruleLogger) EnableDebug(out io.Writer) {
	r.dbg = out
}

// SetConfig populates user configuration of actionlint to the rule.
// When no config is set, rules should behave as if the default configuration is set.
func (r *ruleConfiguration) SetConfig(cfg *Config) {
	r.config = cfg
}

// Config returns the user configuration of actionlint.
// When no config was set to this rule by SetConfig, this method returns nil.
func (r *ruleConfiguration) Config() *Config {
	return r.config
}

type configuredRule interface {
	SetConfig(*Config)
	Config() *Config
}

type (
	debugRule     interface{ EnableDebug(io.Writer) }
	describedRule interface{ Description() string }
)
