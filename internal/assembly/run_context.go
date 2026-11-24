package assembly

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configsource"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// RunContext paths inside WorkingDir are relative to it, and remote configuration addresses omit their query.
type RunContext struct {
	WorkingDir     string
	Repositories   []RepositoryState
	Files          []report.LintedFile
	Configurations []ConfigurationState
	Ignores        []IgnoreEntry
	Providers      []report.ProviderStatus
	Network        NetworkState
	Flags          ResultFlags
	RulesRun       int
	Rules          []RuleOutcome
}

type RepositoryKind int

const (
	NoRepository RepositoryKind = iota
	ReadRepository
	UnreadableRepository
)

// RepositoryState sets Worktree only for ReadRepository, and ChangedFiles are relative to Root.
type RepositoryState struct {
	Kind         RepositoryKind
	Root         string
	Worktree     gitcommand.Worktree
	ReadError    string
	ChangedFiles []string
}

// ConfigurationState leaves File empty when defaults apply and omits output settings from Settings.
type ConfigurationState struct {
	Project   string
	File      string
	Files     []report.LintedFile
	Overrides []string
	Settings  []ChangedSetting
}

// ChangedSetting stores Pointer as a JSON pointer and Value as JSON text.
type ChangedSetting struct {
	Pointer string
	Value   string
	Origin  string
}

type IgnoreSource string

const (
	IgnoreSelectorFlag IgnoreSource = "--ignore"
	IgnoreRegexFlag    IgnoreSource = "--ignore-regex"
	IgnoreConfig       IgnoreSource = "config"
	IgnoreInline       IgnoreSource = "inline"
)

// IgnoreEntry has an empty Location for a flag.
type IgnoreEntry struct {
	Source   IgnoreSource
	Value    string
	Location string
}

// NetworkState stores the name of the token's environment variable in GitHubToken.
type NetworkState struct {
	Offline             bool
	GitHubToken         string
	CacheDirectory      string
	Requests, Failures  uint64
	MetadataCacheHits   uint64
	MetadataCacheMisses uint64
	ResolverCacheHits   uint64
	MetadataAvailable   int
	MetadataUnavailable []ReasonCount
}

type ReasonCount struct {
	Reason string
	Count  int
}

// ResultFlags leaves Experimental nil when neither a flag nor the environment selected it.
type ResultFlags struct {
	Strict       bool
	Experimental *bool
	NoIgnore     bool
}

// RuleOutcome counts Collected findings before publication policy and Reported diagnostics after suppressions, levels and ignores.
type RuleOutcome struct {
	Rule      string
	Collected int
	Reported  int
}

// RunContextInputs uses Getenv to read GH_TOKEN and GITHUB_TOKEN.
type RunContextInputs struct {
	Options         checking.Options
	IgnoreSelectors []string
	Getenv          func(string) string
	Result          *report.Analysis
}

// NewRunContext reads only each repository's Git state and the check's configuration files.
func NewRunContext(ctx context.Context, inputs RunContextInputs) RunContext {
	options, result := inputs.Options, inputs.Result
	if result == nil {
		result = &report.Analysis{}
	}
	run := RunContext{
		WorkingDir:   options.WorkingDir,
		Files:        lintedFiles(options.WorkingDir, result.LintedFiles),
		Repositories: repositoryStates(ctx, options.WorkingDir, result.LintedFiles),
	}
	for _, configuration := range result.Configurations {
		run.Configurations = append(run.Configurations, configurationState(options.WorkingDir, configuration))
	}
	run.Ignores = ignoreEntries(options.WorkingDir, inputs.IgnoreSelectors, options.IgnorePatterns, result)
	// Each repository batch reports its own providers.
	run.Providers = slices.Clone(result.Providers)
	slices.SortStableFunc(run.Providers, func(a, b report.ProviderStatus) int {
		return cmp.Or(strings.Compare(string(a.Kind), string(b.Kind)), strings.Compare(a.Name, b.Name), strings.Compare(a.Inactive, b.Inactive))
	})
	run.Providers = slices.CompactFunc(run.Providers, func(a, b report.ProviderStatus) bool {
		return a.Kind == b.Kind && a.Name == b.Name && a.Version == b.Version && a.Digest == b.Digest &&
			a.OnDemand == b.OnDemand && a.Inactive == b.Inactive && slices.Equal(a.Incomplete, b.Incomplete)
	})
	run.Network = networkState(ctx, inputs.Getenv, result.ActionMetadata)
	run.Flags = ResultFlags{Strict: options.Presets.Strict, NoIgnore: options.NoIgnore}
	if experimental := options.Presets.Experimental; experimental != nil {
		run.Flags.Experimental = new(*experimental)
	}
	run.RulesRun, run.Rules = ruleOutcomes(result)
	return run
}

func relativePath(workingDir, path string) string {
	if configsource.IsRemoteAddress(path) {
		return configsource.DisplayAddress(path)
	}
	if workingDir == "" || !filepath.IsAbs(path) {
		return path
	}
	if relative, err := filepath.Rel(workingDir, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return relative
	}
	return path
}

func lintedFiles(workingDir string, files []report.LintedFile) []report.LintedFile {
	byPath := map[string]string{}
	for _, file := range files {
		byPath[relativePath(workingDir, file.Path)] = file.Digest
	}
	listed := make([]report.LintedFile, 0, len(byPath))
	for _, path := range slices.Sorted(maps.Keys(byPath)) {
		listed = append(listed, report.LintedFile{Path: path, Digest: byPath[path]})
	}
	return listed
}

func repositoryStates(ctx context.Context, workingDir string, files []report.LintedFile) []RepositoryState {
	byRoot := map[string][]string{}
	for _, file := range files {
		if file.Path == "<stdin>" {
			continue
		}
		path := file.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		root := discovery.RepositoryRoot(filepath.Dir(path))
		byRoot[root] = append(byRoot[root], path)
	}
	if len(byRoot) == 0 {
		byRoot[discovery.RepositoryRoot(workingDir)] = nil
	}
	states := make([]RepositoryState, 0, len(byRoot))
	for _, root := range slices.Sorted(maps.Keys(byRoot)) {
		if root == "" {
			states = append(states, RepositoryState{Kind: NoRepository})
			continue
		}
		worktree, err := gitcommand.ReadWorktree(ctx, root)
		if err != nil {
			states = append(states, RepositoryState{Kind: UnreadableRepository, Root: root, ReadError: err.Error()})
			continue
		}
		state := RepositoryState{Kind: ReadRepository, Root: root, Worktree: worktree}
		changed := map[string]bool{}
		for _, path := range worktree.Changes {
			changed[path] = true
		}
		for _, path := range byRoot[root] {
			relative, err := filepath.Rel(root, path)
			if err == nil && changed[filepath.ToSlash(relative)] {
				state.ChangedFiles = append(state.ChangedFiles, relative)
			}
		}
		slices.Sort(state.ChangedFiles)
		state.ChangedFiles = slices.Compact(state.ChangedFiles)
		states = append(states, state)
	}
	return states
}

func configurationState(workingDir string, configuration configreport.Report) ConfigurationState {
	state := ConfigurationState{Project: configuration.Project, File: relativePath(workingDir, configuration.File), Overrides: slices.Clone(configuration.Overrides)}
	configFiles := map[string]bool{}
	if configuration.File != "" {
		configFiles[configuration.File] = true
	}
	origins := configuration.Inspection.Origins
	for _, pointer := range slices.Sorted(maps.Keys(origins)) {
		origin := origins[pointer]
		if origin.Source == "default" || strings.HasPrefix(pointer, "/output/") || pointer == "/output" || hasChildSetting(origins, pointer) {
			continue
		}
		if origin.File != "" {
			configFiles[origin.File] = true
		}
		value, _ := settingValue(configuration.Inspection.Config, pointer)
		state.Settings = append(state.Settings, ChangedSetting{Pointer: pointer, Value: jsonText(value), Origin: originText(workingDir, origin)})
	}
	for _, file := range slices.Sorted(maps.Keys(configFiles)) {
		digest := ""
		if address, err := configsource.NormalizeAddress(file); err == nil {
			digest = configuration.FileDigests[address]
		}
		state.Files = append(state.Files, report.LintedFile{Path: relativePath(workingDir, file), Digest: digest})
	}
	return state
}

func hasChildSetting(origins map[string]configreport.Origin, pointer string) bool {
	for other := range origins {
		if strings.HasPrefix(other, pointer+"/") {
			return true
		}
	}
	return false
}

func originText(workingDir string, origin configreport.Origin) string {
	switch {
	case origin.Input != "":
		return "input " + origin.Input
	case origin.File != "" && origin.Line != 0:
		return relativePath(workingDir, origin.File) + ":" + strconv.Itoa(origin.Line)
	case origin.File != "":
		return relativePath(workingDir, origin.File)
	default:
		return origin.Source
	}
}

func jsonText(value any) string {
	text, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(text)
}

func pointerToken(name string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

func settingValue(config map[string]any, pointer string) (any, bool) {
	var current any = config
	if pointer == "" {
		return current, true
	}
	for token := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.NewReplacer("~1", "/", "~0", "~").Replace(token)
		switch node := current.(type) {
		case map[string]any:
			next, found := node[token]
			if !found {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func ignoreEntries(workingDir string, selectors, patterns []string, result *report.Analysis) []IgnoreEntry {
	var entries []IgnoreEntry
	for _, selector := range selectors {
		entries = append(entries, IgnoreEntry{Source: IgnoreSelectorFlag, Value: selector})
	}
	for _, pattern := range patterns {
		entries = append(entries, IgnoreEntry{Source: IgnoreRegexFlag, Value: pattern})
	}
	seen := map[IgnoreEntry]bool{}
	for _, configuration := range result.Configurations {
		inspection := configuration.Inspection
		if list, ok := inspection.Config["ignores"].([]any); ok {
			for index, item := range list {
				entry := IgnoreEntry{Source: IgnoreConfig, Value: ignoreSelectorText(item)}
				if origin, found := settingOrigin(inspection.Origins, "/ignores/"+strconv.Itoa(index)); found {
					entry.Location = originText(workingDir, origin)
				}
				if !seen[entry] {
					seen[entry] = true
					entries = append(entries, entry)
				}
			}
		}
		messages, _ := settingValue(inspection.Config, "/lint/message-ignores")
		if byPath, ok := messages.(map[string]any); ok {
			for _, glob := range slices.Sorted(maps.Keys(byPath)) {
				list, _ := byPath[glob].([]any)
				for index, pattern := range list {
					entry := IgnoreEntry{Source: IgnoreConfig, Value: glob + ": " + fmt.Sprint(pattern)}
					if origin, found := settingOrigin(inspection.Origins, "/lint/message-ignores/"+pointerToken(glob)+"/"+strconv.Itoa(index)); found {
						entry.Location = originText(workingDir, origin)
					} else if origin, found := settingOrigin(inspection.Origins, "/lint/message-ignores/"+pointerToken(glob)); found {
						entry.Location = originText(workingDir, origin)
					}
					if !seen[entry] {
						seen[entry] = true
						entries = append(entries, entry)
					}
				}
			}
		}
	}
	inline := slices.Clone(result.InlineSuppressions)
	slices.SortFunc(inline, func(a, b report.InlineSuppression) int {
		return cmp.Or(strings.Compare(relativePath(workingDir, a.Path), relativePath(workingDir, b.Path)), cmp.Compare(a.Line, b.Line), strings.Compare(a.Rule, b.Rule))
	})
	for _, directive := range inline {
		entry := IgnoreEntry{Source: IgnoreInline, Value: directive.Rule, Location: relativePath(workingDir, directive.Path) + ":" + strconv.Itoa(directive.Line)}
		if !seen[entry] {
			seen[entry] = true
			entries = append(entries, entry)
		}
	}
	return entries
}

func settingOrigin(origins map[string]configreport.Origin, pointer string) (configreport.Origin, bool) {
	if origin, found := origins[pointer]; found && origin.Source != "default" {
		return origin, true
	}
	var first configreport.Origin
	found := false
	for other, origin := range origins {
		if !strings.HasPrefix(other, pointer+"/") || origin.Source == "default" {
			continue
		}
		if !found || origin.Line < first.Line || origin.Line == first.Line && origin.Column < first.Column {
			first, found = origin, true
		}
	}
	if found {
		return first, true
	}
	for parent := pointer; strings.Contains(parent, "/"); {
		parent = parent[:strings.LastIndex(parent, "/")]
		if origin, exists := origins[parent]; exists && origin.Source != "default" {
			return origin, true
		}
	}
	return configreport.Origin{}, false
}

func ignoreSelectorText(item any) string {
	fields, ok := item.(map[string]any)
	if !ok {
		return jsonText(item)
	}
	var parts []string
	for _, name := range []string{"rule", "file", "uses", "job", "step"} {
		if value, ok := fields[name].(string); ok && value != "" {
			parts = append(parts, name+"="+value)
		}
	}
	return strings.Join(parts, " ")
}

func networkState(ctx context.Context, getenv func(string) string, metadata []actions.Coverage) NetworkState {
	settings := network.Settings(ctx)
	state := NetworkState{Offline: settings.Offline, CacheDirectory: settings.CacheDirectory}
	if getenv != nil && !settings.Offline {
		for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
			if getenv(name) != "" {
				state.GitHubToken = name
				break
			}
		}
	}
	if tracker := accounting.RequestTrackerFromContext(ctx); tracker != nil {
		for _, group := range tracker.Statistics().Requests {
			state.Requests += group.Attempts
			state.Failures += group.Failures
			switch {
			case group.RequestKind == accounting.Cache:
				state.MetadataCacheHits += group.CacheHits
				state.MetadataCacheMisses += group.CacheMisses
			case group.Purpose == "resolver":
				state.ResolverCacheHits += group.CacheHits
			}
		}
	}
	unavailable := map[string]int{}
	seen := map[string]bool{}
	for _, item := range metadata {
		if seen[item.Reference] {
			continue
		}
		seen[item.Reference] = true
		if item.Availability.State() == actions.MetadataUnavailable {
			unavailable[string(item.Availability.Reason())]++
		} else {
			state.MetadataAvailable++
		}
	}
	for _, reason := range slices.Sorted(maps.Keys(unavailable)) {
		state.MetadataUnavailable = append(state.MetadataUnavailable, ReasonCount{Reason: reason, Count: unavailable[reason]})
	}
	return state
}

func ruleOutcomes(result *report.Analysis) (int, []RuleOutcome) {
	collected := map[string]int{}
	for _, item := range result.CollectedByRule {
		collected[item.Rule] += item.Findings
	}
	reported := map[string]int{}
	for _, diagnostic := range result.Diagnostics {
		reported[diagnostic.Rule]++
	}
	names := map[string]bool{}
	for name := range collected {
		names[name] = true
	}
	for name := range reported {
		names[name] = true
	}
	var outcomes []RuleOutcome
	for _, name := range slices.Sorted(maps.Keys(names)) {
		if collected[name] != 0 || reported[name] != 0 {
			outcomes = append(outcomes, RuleOutcome{Rule: name, Collected: collected[name], Reported: reported[name]})
		}
	}
	return len(collected), outcomes
}
