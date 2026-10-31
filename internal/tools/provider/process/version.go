package process

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// VersionProbe holds the result of one version probe for every backend that
// shares it, so a command is probed and its failure warned about once. Each
// caller keeps its own cancellation: an interrupted probe leaves the next
// caller to run it again. A completed failure is kept as text, so every
// caller gets its own error. The zero value is ready to use.
type VersionProbe struct {
	mu      sync.Mutex
	pending chan struct{}
	ready   bool
	version string
	failure string
	failed  bool
}

// run returns the shared result, running execute when no probe completed. A
// completed failure is passed to warning once.
func (p *VersionProbe) run(ctx context.Context, execute func() (string, error), warning func(error)) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p.mu.Lock()
		if p.ready {
			version, failure, failed := p.version, p.failure, p.failed
			p.mu.Unlock()
			if failed {
				return version, errors.New(failure)
			}
			return version, nil
		}
		if pending := p.pending; pending != nil {
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		p.pending = pending
		p.mu.Unlock()
		version, err := execute()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		completed := !canceled(err)
		p.mu.Lock()
		if completed {
			p.ready, p.version, p.failed = true, version, err != nil
			if err != nil {
				p.failure = err.Error()
			}
		}
		p.pending = nil
		close(pending)
		p.mu.Unlock()
		if completed && err != nil && warning != nil {
			warning(err)
		}
		return version, err
	}
}

func canceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// identifyVersion extracts the version from probe output and checks it
// against minimum, when set.
func identifyVersion(name string, pattern *regexp.Regexp, minimum, output string) (string, error) {
	match := pattern.FindStringSubmatch(output)
	if match == nil {
		if minimum != "" {
			return "", fmt.Errorf("could not identify %s version; requires %s or newer: %q", name, minimum, strings.TrimSpace(output))
		}
		return "", fmt.Errorf("could not identify %s version: %q", name, strings.TrimSpace(output))
	}
	version := match[1]
	if minimum == "" {
		return version, nil
	}
	order, err := compareVersions(version, minimum)
	if err != nil {
		return version, fmt.Errorf("%s version %q: %w", name, version, err)
	}
	if order < 0 {
		return version, fmt.Errorf("%s %q is incompatible; requires %s or newer", name, version, minimum)
	}
	return version, nil
}

// compareVersions orders dotted numeric versions with an optional
// "-prerelease" and "+build" suffix by semantic-versioning precedence:
// missing numeric parts are zero, a prerelease precedes its release and build
// metadata is ignored.
func compareVersions(a, b string) (int, error) {
	left, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range max(len(left.release), len(right.release)) {
		var x, y uint64
		if i < len(left.release) {
			x = left.release[i]
		}
		if i < len(right.release) {
			y = right.release[i]
		}
		if x != y {
			return cmp.Compare(x, y), nil
		}
	}
	return comparePrerelease(left.prerelease, right.prerelease), nil
}

type parsedVersion struct {
	release    []uint64
	prerelease []string
}

func parseVersion(version string) (parsedVersion, error) {
	version, _, _ = strings.Cut(version, "+")
	core, prerelease, hasPrerelease := strings.Cut(version, "-")
	var parsed parsedVersion
	for part := range strings.SplitSeq(core, ".") {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return parsedVersion{}, fmt.Errorf("version %q is not dotted numbers", version)
		}
		parsed.release = append(parsed.release, number)
	}
	if hasPrerelease {
		if prerelease == "" {
			return parsedVersion{}, fmt.Errorf("version %q has an empty prerelease", version)
		}
		parsed.prerelease = strings.Split(prerelease, ".")
	}
	return parsed, nil
}

// comparePrerelease orders prerelease identifiers; none ranks highest.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := range min(len(a), len(b)) {
		x, xErr := strconv.ParseUint(a[i], 10, 64)
		y, yErr := strconv.ParseUint(b[i], 10, 64)
		switch {
		case xErr == nil && yErr == nil && x != y:
			return cmp.Compare(x, y)
		case xErr == nil && yErr != nil:
			return -1
		case xErr != nil && yErr == nil:
			return 1
		case xErr != nil && yErr != nil && a[i] != b[i]:
			return strings.Compare(a[i], b[i])
		}
	}
	return cmp.Compare(len(a), len(b))
}
