package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func commitHash(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func gitRemoteRefs(ctx context.Context, slug string) (map[string]string, error) {
	if len(strings.Split(slug, "/")) != 2 || strings.ContainsAny(slug, " :?\\\r\n") {
		return nil, fmt.Errorf("invalid repository %q", slug)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "ls-remote", "--tags", "--heads", "https://github.com/"+slug+".git")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", slug, err)
	}
	refs := map[string]string{}
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && commitHash(fields[0]) {
			refs[fields[1]] = fields[0]
		}
	}
	return refs, nil
}

func pinRegistry(ctx context.Context, actions []*registry, resolve func(context.Context, string) (map[string]string, error)) error {
	repositories := map[string]map[string]string{}
	for _, action := range actions {
		refs, exists := repositories[action.Slug]
		if !exists {
			var err error
			refs, err = resolve(ctx, action.Slug)
			if err != nil {
				return err
			}
			repositories[action.Slug] = refs
		}
		action.Pins = map[string]string{}
		for _, tag := range action.Tags {
			pin := refs["refs/tags/"+tag+"^{}"]
			if pin == "" {
				pin = refs["refs/tags/"+tag]
			}
			if pin == "" {
				pin = refs["refs/heads/"+tag]
			}
			if !commitHash(pin) {
				return fmt.Errorf("no immutable commit found for %s@%s", action.Slug, tag)
			}
			action.Pins[tag] = pin
		}
	}
	return nil
}
