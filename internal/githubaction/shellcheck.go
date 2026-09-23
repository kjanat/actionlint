package githubaction

import (
	"errors"
	"io"
	"os"
	"strings"

	"actionlint.kjanat.dev"
	"go.yaml.in/yaml/v4"
)

func (req *lintRequest) configureShellcheck(env func(string) string, workspace string) error {
	if req.shellcheck == "" {
		return nil
	}
	args, err := shellcheckArguments(env("INPUT_SHELLCHECK-ARGS"))
	if err != nil {
		return err
	}
	// ShellCheck splits SHELLCHECK_OPTS on spaces, without shell quote expansion.
	var inherited []string
	for arg := range strings.SplitSeq(env("SHELLCHECK_OPTS"), " ") {
		if arg != "" {
			inherited = append(inherited, arg)
		}
	}
	flags, err := mergeShellcheckFlags(inherited, args)
	if err != nil {
		return err
	}
	switch config := strings.TrimSpace(env("INPUT_SHELLCHECK-CONFIG")); config {
	case "": // Retain flag selection, or inherit the project configuration.
	case "false":
		flags.settings.Config = actionlint.ShellcheckRCDisabled
	case "true":
		flags.settings.Config = actionlint.ShellcheckRCDiscover
	default:
		flags.settings.Config = actionlint.ShellcheckRCFile(config)
	}
	if config, ok := flags.settings.Config.(actionlint.ShellcheckRCFile); ok {
		path, err := shellcheckConfigPath(workspace, string(config))
		if err != nil {
			return err
		}
		flags.settings.Config = actionlint.ShellcheckRCFile(path)
	}
	req.shellcheckSettings = &flags.settings
	if req.shellcheckOptions == nil {
		req.shellcheckOptions = &actionlint.ExternalCommandOptions{}
	}
	req.shellcheckOptions.Arguments = append(req.shellcheckOptions.Arguments, flags.arguments()...)
	// Pass the validated defaults once, before the explicit input arguments.
	req.shellcheckOptions.Environment = append(req.shellcheckOptions.Environment, "SHELLCHECK_OPTS=")
	return nil
}

func shellcheckConfigPath(workspace, config string) (string, error) {
	path := inputPath(workspace, config)
	file, err := os.Open(path)
	if err != nil {
		return "", inputErrorf("ShellCheck config: %s", err)
	}
	info, err := file.Stat()
	_ = file.Close()
	if err != nil || !info.Mode().IsRegular() {
		return "", inputErrorf("ShellCheck config must identify a readable regular file")
	}
	return path, nil
}

func shellcheckArguments(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(value))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return nil, inputErrorf("Input 'shellcheck-args' must be a YAML/JSON array of strings: %s", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil, inputErrorf("Input 'shellcheck-args' must be a YAML/JSON array of strings")
	}
	var args []string
	for _, node := range doc.Content[0].Content {
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			return nil, inputErrorf("Input 'shellcheck-args' entries must be strings")
		}
		args = append(args, node.Value)
	}
	if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, inputErrorf("Input 'shellcheck-args' must contain exactly one YAML/JSON array")
	}
	return args, nil
}
