package cli

import (
	"fmt"
	"strconv"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"github.com/spf13/cobra"
)

type configFormatFlag struct {
	selected *repoconfig.FileFormat
	format   repoconfig.FileFormat
}

func (flag *configFormatFlag) Set(value string) error {
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	if !enabled {
		if *flag.selected == flag.format {
			*flag.selected = ""
		}
		return nil
	}
	if *flag.selected != "" && *flag.selected != flag.format {
		return fmt.Errorf("--%s cannot be combined with --%s", flag.format, *flag.selected)
	}
	*flag.selected = flag.format
	return nil
}

func (flag *configFormatFlag) String() string {
	return strconv.FormatBool(*flag.selected == flag.format)
}
func (*configFormatFlag) Type() string { return "bool" }

func (a *commandApp) configInitFlags(command *cobra.Command) {
	for _, format := range repoconfig.FileFormats() {
		name := string(format)
		command.Flags().Var(&configFormatFlag{selected: &a.inv.ConfigFormat, format: format}, name, "Create "+format.Filename())
		command.Flags().Lookup(name).NoOptDefVal = "true"
		annotateFlag(command.Flags(), name, "Configuration")
	}
	command.Example = "  actionlint config init --toml\n  actionlint config init --jsonc"
}
