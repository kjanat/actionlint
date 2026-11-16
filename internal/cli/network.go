package cli

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/cli/arguments"
	"actionlint.kjanat.dev/v2/internal/cli/streams"
)

func (a *commandApp) prepareNetwork() error {
	if a.set["online"] && a.set["offline"] {
		return arguments.UsageError{Cause: errors.New("--online and --offline cannot be combined")}
	}
	if a.set["online"] {
		a.inv.Check.Offline = !a.opts.online
	}
	if a.inv.Check.WarmCache && a.inv.Check.Offline {
		return arguments.UsageError{Cause: errors.New("cache warm requires network access; omit --offline")}
	}
	return nil
}

func (a *commandApp) reportWarning(message string) {
	if a.errorJSON || a.jsonOutput() {
		_ = streams.WriteJSON(a.streams.Stderr, struct {
			Warning string `json:"warning"`
		}{message})
		return
	}
	_, _ = fmt.Fprintln(a.streams.Stderr, "warning: "+message)
}
