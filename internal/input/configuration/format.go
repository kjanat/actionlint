package config

// FileFormat selects the syntax and extension of a configuration file.
type FileFormat string

const (
	FormatYAML  FileFormat = "yaml"
	FormatYML   FileFormat = "yml"
	FormatJSON  FileFormat = "json"
	FormatJSONC FileFormat = "jsonc"
	FormatTOML  FileFormat = "toml"
)

// FileFormats lists configuration formats in repository discovery order.
func FileFormats() []FileFormat {
	return []FileFormat{FormatYAML, FormatYML, FormatJSON, FormatJSONC, FormatTOML}
}

func (format FileFormat) Filename() string {
	if format == "" {
		format = FormatYAML
	}
	return "actionlint." + string(format)
}
