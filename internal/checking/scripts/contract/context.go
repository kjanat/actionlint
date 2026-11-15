package tools

import "fmt"

// Detached gives one adapter owned settings and input data. Explicit runtime
// capabilities remain shared for the duration of the invocation.
func (c Context) Detached() (Context, error) {
	var err error
	c.Config, err = c.Config.Detached()
	if err != nil {
		return Context{}, fmt.Errorf("copy tool configuration: %w", err)
	}
	c.ProjectConfig, err = c.ProjectConfig.Detached()
	if err != nil {
		return Context{}, fmt.Errorf("copy tool project configuration: %w", err)
	}
	c.Document = c.Document.Detached()
	return c, nil
}
