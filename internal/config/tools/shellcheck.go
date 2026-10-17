package tools

// ShellcheckSettings overrides ShellCheck's workflow-derived defaults.
// A nil Config inherits the project selection; absent project config disables rc discovery.
type ShellcheckSettings struct {
	Config          ShellcheckConfigSelection
	Shell           string
	Exclude         []string
	ExternalSources *bool
}

// ShellcheckConfigSelection selects an rc file, discovery, or disabled rc loading.
type ShellcheckConfigSelection interface{ shellcheckConfigSelection() }

// ShellcheckRCFile selects an explicit rc file.
// Relative application paths use the process working directory. Path
// interpolations use the analyzed project's configuration and action context.
type ShellcheckRCFile string

func (ShellcheckRCFile) shellcheckConfigSelection() {}

// ShellcheckRCMode selects discovery or disables rc loading.
type ShellcheckRCMode bool

func (ShellcheckRCMode) shellcheckConfigSelection() {}

const (
	// ShellcheckRCDisabled disables rc loading, including project-selected paths.
	ShellcheckRCDisabled ShellcheckRCMode = false
	// ShellcheckRCDiscover discovers configuration from the analysis working directory.
	ShellcheckRCDiscover ShellcheckRCMode = true
)
