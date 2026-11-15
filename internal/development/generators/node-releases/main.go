// Command node-releases updates the vendored Node.js support schedule from a pinned commit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func main() {
	revision := flag.String("revision", "", "Full commit SHA in nodejs/Release")
	output := flag.String("output", "internal/model/runtimes/data", "Output directory")
	flag.Parse()
	if err := update(*revision, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func update(revision, output string) error {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) {
		return errors.New("-revision requires a full lowercase commit SHA")
	}
	address := "https://raw.githubusercontent.com/nodejs/Release/" + revision + "/schedule.json"
	client := &http.Client{Timeout: 30 * time.Second}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch schedule: %s", response.Status)
	}
	const maxBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return errors.New("schedule exceeds 1 MiB")
	}
	var records map[string]struct{ Start, End string }
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	if len(records) == 0 {
		return errors.New("empty schedule")
	}
	for version, record := range records {
		start, err := time.Parse("2006-01-02", record.Start)
		if err != nil {
			return fmt.Errorf("%s start: %w", version, err)
		}
		end, err := time.Parse("2006-01-02", record.End)
		if err != nil {
			return fmt.Errorf("%s end: %w", version, err)
		}
		if !start.Before(end) {
			return fmt.Errorf("%s has an invalid support interval", version)
		}
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "node-schedule.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "node-source.txt"), []byte(address+"\n"), 0o644)
}
