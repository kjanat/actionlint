package actionlint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

// analyzeCommand resolves filesystem and configuration choices before invoking analysis.
func analyzeCommand(ctx context.Context, stdin io.Reader, call checkInvocation, log io.Writer, quiet bool) (*AnalysisResult, []string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	projects := &Projects{skipConfig: call.Config.Disabled || call.Config.Path != ""}
	var explicit *Config
	if call.Config.Path != "" {
		explicit, err = ReadConfigFile(call.Config.Path)
		if err != nil {
			return nil, nil, err
		}
	}
	request := AnalysisRequest{ShellCheck: call.ShellCheck, Pyflakes: call.Pyflakes}
	request.IgnorePatterns, err = compileIgnorePatterns(call.IgnoreRegex)
	if err != nil {
		return nil, nil, err
	}

	paths := call.Paths
	if len(paths) == 0 {
		project, e := projects.At(cwd)
		if e != nil {
			return nil, nil, e
		}
		if project == nil {
			return nil, nil, fmt.Errorf("no project was found in any parent directories of %q. check workflows directory is put correctly in your Git repository", cwd)
		}
		dir := project.WorkflowsDir()
		err = filepath.Walk(dir, func(path string, info os.FileInfo, e error) error {
			if e != nil {
				return e
			}
			if !info.IsDir() && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")) {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("could not read files in %q: %w", dir, err)
		}
		if len(paths) == 0 {
			return nil, nil, fmt.Errorf("no YAML file was found in %q", dir)
		}
	}
	inputs := []string{}
	for _, path := range paths {
		var source []byte
		var project *Project
		if len(paths) == 1 && path == "-" {
			path = call.StdinFilename
			source, err = io.ReadAll(stdin)
			if err != nil {
				return nil, nil, fmt.Errorf("could not read stdin: %w", err)
			}
			if _, e := os.Stat(path); !errors.Is(e, os.ErrNotExist) {
				project, err = projects.At(path)
			}
		} else {
			project, err = projects.At(path)
			if err == nil {
				source, err = os.ReadFile(path)
				if err != nil {
					err = fmt.Errorf("could not read %q: %w", path, err)
				}
			}
		}
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, path)
		cfg := explicit
		if cfg == nil && project != nil {
			cfg = project.Config()
		}
		if rel, e := filepath.Rel(cwd, path); e == nil {
			path = rel
		}
		request.Sources = append(request.Sources, SourceUnit{Path: path, Content: source, Config: cfg, Project: project})
	}
	for _, project := range projects.known {
		if project.Config() != nil {
			for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
				inputs = append(inputs, filepath.Join(project.RootDir(), ".github", name))
			}
		}
	}
	level := LogLevelNone
	if !quiet {
		if call.Verbose {
			level = LogLevelVerbose
		} else if call.Debug {
			level = LogLevelDebug
		}
	}
	result, err := analyze(ctx, request, log, level)
	if err != nil {
		return nil, nil, err
	}
	return result, append(inputs, result.Inputs...), nil
}

func renderAnalysis(out io.Writer, result *AnalysisResult, options renderOptions) error {
	if options.Format == OutputFormatJSON || options.Format == OutputFormatJSONL {
		return writeDiagnostics(out, result.Diagnostics, options.Format == OutputFormatJSONL)
	}
	previous := color.NoColor
	defer func() { color.NoColor = previous }()
	switch options.Color {
	case ColorOptionKindNever:
		color.NoColor = true
	case ColorOptionKindAlways:
		color.NoColor = false
	}
	if file, ok := out.(*os.File); ok && !color.NoColor {
		out = colorable.NewColorable(file)
	}
	writes := &commandResultWriter{Writer: out}
	var formatter diagnosticFormatter
	template := options.Template
	if options.Format == OutputFormatSARIF {
		template = SARIFTemplate()
	}
	if template != "" {
		f, err := NewErrorFormatter(template)
		if err != nil {
			return err
		}
		for _, file := range result.files {
			for _, rule := range file.rules {
				f.RegisterRule(rule)
			}
		}
		formatter = f
	} else if options.Format == OutputFormatGitHub {
		formatter = githubDiagnosticFormatter{}
	}
	sources := map[string][]byte{}
	for _, file := range result.files {
		sources[file.source.Path] = file.source.Content
		for _, finding := range file.errors {
			if finding.source != nil {
				sources[finding.Filepath] = finding.source
			}
		}
	}
	fields := []*ErrorTemplateFields{}
	for _, diagnostic := range result.Diagnostics {
		finding := diagnostic.legacyError()
		source := sources[diagnostic.Path]
		if formatter != nil {
			fields = append(fields, finding.GetTemplateFields(source))
		} else {
			if options.Oneline || options.Format == OutputFormatOneline {
				source = nil
			}
			finding.PrettyPrint(writes, source)
		}
	}

	if formatter != nil {
		if err := formatter.Print(writes, fields); err != nil {
			return err
		}
	}
	return writes.err
}
