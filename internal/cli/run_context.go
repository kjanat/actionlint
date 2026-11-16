package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/assembly"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// shortDigestLength keeps digests readable while distinguishing revisions.
const shortDigestLength = 12

// writeRunContext prints the verbose description of what determined a check's
// findings, one "verbose:" line per fact. It is the only place the run context
// is formatted.
func writeRunContext(w io.Writer, run assembly.RunContext) error {
	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }

	for _, repository := range run.Repositories {
		switch repository.Kind {
		case assembly.NoRepository:
			add("repository: none")
		case assembly.UnreadableRepository:
			add("repository: %s (unreadable: %s)", repository.Root, repository.ReadError)
		case assembly.ReadRepository:
			head := "no commits"
			if repository.Worktree.Head != "" {
				head = "HEAD " + shortDigest(repository.Worktree.Head)
			}
			tree := "clean"
			if repository.Worktree.Dirty() {
				tree = fmt.Sprintf("dirty, %d changed paths", len(repository.Worktree.Changes))
			}
			add("repository: %s at %s (%s)", repository.Root, head, tree)
			for _, path := range repository.ChangedFiles {
				add("  uncommitted linted file: %s", path)
			}
		}
	}
	for _, file := range run.Files {
		add("file: %s %s", file.Path, digestText(file.Digest))
	}

	if len(run.Configurations) == 0 {
		add("config: none, defaults")
	}
	for _, configuration := range run.Configurations {
		project := ""
		if configuration.Project != "" {
			project = " for project " + configuration.Project
		}
		if configuration.File == "" {
			add("config: none, defaults%s", project)
		} else {
			add("config: %s%s", configuration.File, project)
		}
		for _, file := range configuration.Files {
			add("  config file: %s %s", file.Path, digestText(file.Digest))
		}
		if len(configuration.Overrides) != 0 {
			add("  config overrides: %s", strings.Join(configuration.Overrides, ", "))
		}
		for _, setting := range configuration.Settings {
			add("  setting: %s = %s (%s)", setting.Pointer, setting.Value, setting.Origin)
		}
	}

	if len(run.Ignores) == 0 {
		add("ignore: none")
	}
	for _, entry := range run.Ignores {
		if entry.Location == "" {
			add("ignore: %s %s", entry.Source, entry.Value)
		} else {
			add("ignore: %s %s (%s)", entry.Source, entry.Value, entry.Location)
		}
	}

	for _, provider := range run.Providers {
		add("%s", providerText(provider))
	}

	network := run.Network
	access := "online"
	if network.Offline {
		access = "offline"
	}
	token := "none"
	if network.GitHubToken != "" {
		token = "from " + network.GitHubToken
	}
	cache := network.CacheDirectory
	if cache == "" {
		cache = "none"
	}
	add("network: %s, GitHub token %s, cache %s", access, token, cache)
	add("network requests: %d attempts, %d failed; metadata cache: %d hits, %d misses; resolver cache hits: %d",
		network.Requests, network.Failures, network.MetadataCacheHits, network.MetadataCacheMisses, network.ResolverCacheHits)
	unavailable := 0
	reasons := make([]string, 0, len(network.MetadataUnavailable))
	for _, reason := range network.MetadataUnavailable {
		unavailable += reason.Count
		reasons = append(reasons, reason.Reason+" "+strconv.Itoa(reason.Count))
	}
	if unavailable == 0 {
		add("remote metadata: %d available", network.MetadataAvailable)
	} else {
		add("remote metadata: %d available, %d unavailable (%s)", network.MetadataAvailable, unavailable, strings.Join(reasons, ", "))
	}

	experimental := "default"
	if run.Flags.Experimental != nil {
		experimental = strconv.FormatBool(*run.Flags.Experimental)
	}
	add("flags: strict=%t experimental=%s no-ignore=%t; rules run: %d", run.Flags.Strict, experimental, run.Flags.NoIgnore, run.RulesRun)
	for _, rule := range run.Rules {
		add("rule %s: %d collected, %d reported", rule.Rule, rule.Collected, rule.Reported)
	}

	var text strings.Builder
	for _, line := range lines {
		text.WriteString("verbose: ")
		text.WriteString(line)
		text.WriteByte('\n')
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func shortDigest(digest string) string {
	if len(digest) > shortDigestLength {
		return digest[:shortDigestLength]
	}
	return digest
}

func digestText(digest string) string {
	if digest == "" {
		return "(not hashed)"
	}
	return "sha256:" + shortDigest(digest)
}

func providerText(provider report.ProviderStatus) string {
	text := string(provider.Kind) + ": " + provider.Name
	if provider.Version != "" {
		text += " " + provider.Version
	}
	if provider.Digest != "" {
		text += " sha256:" + shortDigest(provider.Digest)
	}
	if provider.OnDemand {
		text += " on demand"
	}
	if provider.Inactive != "" {
		return text + " inactive (" + provider.Inactive + ")"
	}
	return text + " active"
}
