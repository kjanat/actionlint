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

// RunContext describes the inputs and settings that determined one check's
// findings. Compare two runs' contexts to explain different results. Paths
// inside WorkingDir are relative to it; remote configuration addresses omit
// their query.
type RunContext struct {
	WorkingDir     string
	Repositories   []RepositoryState
	Files          []report.LintedFile
	Configurations []ConfigurationState
	Ignores        []IgnoreEntry
	Providers      []report.ProviderStatus
	Network        NetworkState
	Flags          ResultFlags
	// RulesRun counts the distinct rules that ran.
	RulesRun int
	// Rules lists the rules that collected or reported findings, by name.
	Rules []RuleOutcome
}

// RepositoryKind tells whether linted files belong to a Git repository and
// whether its state could be read.
type RepositoryKind int

const (
	// NoRepository groups linted files outside any Git repository.
	NoRepository RepositoryKind = iota
	// ReadRepository is a repository whose HEAD and status were read.
	ReadRepository
	// UnreadableRepository is a repository Git could not read; ReadError says why.
	UnreadableRepository
)

// RepositoryState is the Git state of the repository that contains some of the
// linted files. Root is empty for NoRepository. Worktree is set for
// ReadRepository. ChangedFiles lists the linted files with uncommitted
// changes, relative to Root.
type RepositoryState struct {
	Kind         RepositoryKind
	Root         string
	Worktree     gitcommand.Worktree
	ReadError    string
	ChangedFiles []string
}

// ConfigurationState is the configuration one project used. File is empty when
// defaults apply. Files lists every local configuration file that supplied a
// setting, with its digest. Settings lists the settings that differ from the
// defaults, except output settings, which do not change findings.
type ConfigurationState struct {
	Project   string
	File      string
	Files     []report.LintedFile
	Overrides []string
	Settings  []ChangedSetting
}

// ChangedSetting is one configuration value that differs from its default.
// Pointer is its JSON pointer, Value its JSON text and Origin the file and line
// that set it, or the input that supplied it.
type ChangedSetting struct {
	Pointer string
	Value   string
	Origin  string
}

// IgnoreSource names where an ignore entry came from.
type IgnoreSource string

const (
	IgnoreSelectorFlag IgnoreSource = "--ignore"
	IgnoreRegexFlag    IgnoreSource = "--ignore-regex"
	IgnoreConfig       IgnoreSource = "config"
	IgnoreInline       IgnoreSource = "inline"
)

// IgnoreEntry is one selector, pattern or directive that can remove findings.
// Location is the file and line that declared it; it is empty for flags.
type IgnoreEntry struct {
	Source   IgnoreSource
	Value    string
	Location string
}

// NetworkState describes network access and the reuse of cached remote data.
// GitHubToken names the environment variable that supplied a token, never the
// token itself, and is empty when none was used. Requests, Failures and the
// cache counts come from the invocation's request accounting.
type NetworkState struct {
	Offline             bool
	GitHubToken         string
	CacheDirectory      string
	Requests, Failures  uint64
	MetadataCacheHits   uint64
	MetadataCacheMisses uint64
	ResolverCacheHits   uint64
	MetadataAvailable   int
	// MetadataUnavailable counts unavailable remote metadata by reason.
	MetadataUnavailable []ReasonCount
}

// ReasonCount counts the outcomes that share one reason.
type ReasonCount struct {
	Reason string
	Count  int
}

// ResultFlags are the invocation presets that change findings. Experimental
// is nil when neither a flag nor the environment selected it.
type ResultFlags struct {
	Strict       bool
	Experimental *bool
	NoIgnore     bool
}

// RuleOutcome counts one rule's findings before publication policy and the
// diagnostics reported after inline suppressions, levels and ignore settings.
type RuleOutcome struct {
	Rule      string
	Collected int
	Reported  int
}

// RunContextInputs are the values NewRunContext describes. Options are the
// checking options the check ran with. IgnoreSelectors are the --ignore values
// as given. Getenv reads GH_TOKEN and GITHUB_TOKEN.
type RunContextInputs struct {
	Options         checking.Options
	IgnoreSelectors []string
	Getenv          func(string) string
	Result          *report.Analysis
}

// NewRunContext describes a completed check. It reads each repository's Git
// state and the configuration files the check used, and nothing else.
func NewRunContext(ctx context.Context, inputs RunContextInputs) RunContext {
	options, result := inputs.Options, inputs.Result
	if result == nil {
		result = &report.Analysis{}
	}
	run := RunContext{WorkingDir: options.WorkingDir}
	run.Files = lintedFiles(options.WorkingDir, result.LintedFiles)
	run.Repositories = repositoryStates(ctx, options.WorkingDir, result.LintedFiles)
	for _, configuration := range result.Configurations {
		run.Configurations = append(run.Configurations, configurationState(options.WorkingDir, configuration))
	}
	run.Ignores = ignoreEntries(options.WorkingDir, inputs.IgnoreSelectors, options.IgnorePatterns, result)
	// Each repository batch reports its own providers; list each status once.
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

// relativePath returns path for display: relative to workingDir when inside
// it, and without the query when it is a remote configuration address.
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

// lintedFiles returns one entry per linted path, sorted by path.
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

// repositoryStates groups linted files by the Git repository that contains
// them and reads each repository's HEAD and status once.
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

// hasChildSetting reports whether origins records a setting below pointer.
// Such a pointer selects a container of settings.
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

// pointerToken escapes one JSON pointer reference token.
func pointerToken(name string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

// settingValue reads the value a JSON pointer selects in an effective
// configuration, whose objects are maps and whose arrays are slices.
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

// ignoreEntries lists the ignore flags, the configured ignores and message
// ignores, and the inline directives in analyzed sources.
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

// settingOrigin returns the non-default origin of the setting at pointer. When
// only its members record origins, it returns the first of those; when only
// an enclosing setting does, such as a whole list, it returns that one.
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

// ignoreSelectorText renders an ignores entry's selector fields in a stable order.
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
