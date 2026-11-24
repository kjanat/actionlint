package checkinputs

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

type ToolIntegration struct {
	Tool                string
	Contract            string
	ConfigurationSchema string
}

// ToolIntegrator leaves plugin options to the guest, and a nil descriptor
// means the optional interface is absent.
type ToolIntegrator interface {
	ToolIntegration() *ToolIntegration
	NegotiateTool(context.Context, json.RawMessage, json.RawMessage) (*ToolNegotiation, error)
}

type ToolResourceKind string

const (
	ToolWorkspace ToolResourceKind = "workspace"
	ToolDirectory ToolResourceKind = "directory"
)

// ToolResourceRequest sets Path only for ToolDirectory.
type ToolResourceRequest struct {
	Kind ToolResourceKind
	Path string
}

type ToolNegotiation struct {
	Options   json.RawMessage
	Resources []ToolResourceRequest
}

// UnsupportedToolConfigurationError permits falling back to the native tool.
type UnsupportedToolConfigurationError struct{ Reason string }

func (e *UnsupportedToolConfigurationError) Error() string { return e.Reason }

type SourceRoot struct {
	Host  string
	Guest string
}

type ShellScriptLineKind string

const (
	ShellScriptSourceLine    ShellScriptLineKind = "source"
	ShellScriptCommentLine   ShellScriptLineKind = "comment"
	ShellScriptGeneratedLine ShellScriptLineKind = "generated"
)

type ShellScriptLine struct {
	Kind     ShellScriptLineKind
	Line     int
	Position sourcetext.Position
}
type ShellScriptInput struct {
	Text       string
	MaskedText string
	Dialect    *string
	Shell      string
	ShellSetup string
	Lines      []ShellScriptLine
	Excluded   []uint32
}
type ShellScriptInputBuilder func(context.Context, invocation.ID, *string) (*ShellScriptInput, error)
