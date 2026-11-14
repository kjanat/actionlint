package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	cachemanagement "actionlint.kjanat.dev/v2/internal/integrations/cache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"github.com/spf13/cobra"
)

type cacheInvocation struct {
	DryRun    bool
	OlderThan time.Duration
}

func (a *commandApp) cacheCommand() *cobra.Command {
	parent := &cobra.Command{Use: "cache", Short: "Download, inspect and remove cached dependencies"}
	warm := &cobra.Command{
		Use: "warm [flags] [files...]", Short: "Cache configuration, plugins and remote actions for the selected files", Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			a.inv.Check.WarmCache = true
			return a.capture(operationCheck)(c, args)
		},
	}
	a.checkFlags(warm, true)
	parent.AddCommand(warm)
	for _, item := range []struct {
		name, description string
		op                operation
	}{
		{"dir", "Print the cache directory", operationCacheDir},
		{"list", "List cached files, sizes and modification times", operationCacheList},
		{"prune", "Remove old downloads and compiled modules", operationCachePrune},
		{"clean", "Remove downloaded dependencies and compiled modules", operationCacheClean},
	} {
		command := &cobra.Command{Use: item.name, Short: item.description, Args: cobra.NoArgs, RunE: a.capture(item.op)}
		a.commonFlags(command, command.Flags())
		if item.op == operationCachePrune || item.op == operationCacheClean {
			command.Flags().BoolVar(&a.inv.Cache.DryRun, "dry-run", false, "List files that would be removed without changing the cache")
			annotateFlag(command.Flags(), "dry-run", "Cache")
		}
		if item.op == operationCachePrune {
			command.Flags().DurationVar(&a.inv.Cache.OlderThan, "older-than", 30*24*time.Hour, "Remove files last written before this age, for example 24h")
			annotateFlag(command.Flags(), "older-than", "Cache")
		}
		parent.AddCommand(command)
	}
	return parent
}

type cacheRequest struct {
	Operation operation
	cacheInvocation
	JSON bool
}

func (r cacheRequest) run(ctx context.Context, streams Command) (int, error) {
	directory := network.Settings(ctx).CacheDirectory
	if r.Operation == operationCacheDir {
		if r.JSON {
			return 0, commandstreams.WriteJSON(streams.Stdout, struct {
				Directory string `json:"directory"`
			}{directory})
		}
		_, err := fmt.Fprintln(streams.Stdout, directory)
		return 0, err
	}
	if r.Operation == operationCacheList {
		inventory, err := cachemanagement.Inspect(ctx, directory)
		if err != nil {
			return 0, err
		}
		if r.JSON {
			return 0, commandstreams.WriteJSON(streams.Stdout, inventory)
		}
		return 0, writeCacheInventory(streams.Stdout, inventory)
	}
	var cutoff time.Time
	if r.Operation == operationCachePrune {
		cutoff = time.Now().Add(-r.OlderThan)
	}
	result, err := cachemanagement.Clean(ctx, directory, cutoff, r.DryRun)
	if err != nil {
		if r.DryRun {
			return 0, fmt.Errorf("cache preview failed: %w", err)
		}
		return 0, fmt.Errorf("cache cleanup stopped after removing %d files (%d bytes): %w", result.Files, result.Bytes, err)
	}
	if r.JSON {
		return 0, commandstreams.WriteJSON(streams.Stdout, result)
	}
	if r.DryRun {
		if err := writeCacheInventory(streams.Stdout, result.Inventory); err != nil {
			return 0, err
		}
		_, err = fmt.Fprintln(streams.Stdout, "Dry run. No files were removed.")
	} else {
		_, err = fmt.Fprintf(streams.Stdout, "Removed %d files (%d bytes) from %s\n", result.Files, result.Bytes, directory)
	}
	return 0, err
}

func writeCacheInventory(out io.Writer, inventory cachemanagement.Inventory) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "TYPE\tBYTES\tMODIFIED\tFILE"); err != nil {
		return err
	}
	for _, entry := range inventory.Entries {
		if _, err := fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", entry.Kind, entry.Bytes, entry.ModifiedAt.UTC().Format(time.RFC3339), entry.Path); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "%d files, %d bytes in %s\n", inventory.Files, inventory.Bytes, inventory.Directory)
	return err
}
