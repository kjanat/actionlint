package ruff

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Compatibility shares one automatic-discovery probe across an analysis.
type Compatibility struct {
	once sync.Once
	err  error
}

// CheckCompatibility reports the same version-probe failure used by optional
// discovery, without converting it into a skipped check and warning.
func CheckCompatibility(ctx context.Context, run Run, wait func() error) error {
	var check Compatibility
	var failure error
	_, err := check.available(ctx, run, wait, func(err error) { failure = err })
	if err != nil {
		return err
	}
	return failure
}

func (c *Compatibility) available(ctx context.Context, run Run, wait func() error, warning func(error)) (bool, error) {
	c.once.Do(func() {
		run([]string{"--version"}, "", func(output []byte, err error) error {
			if err != nil {
				c.err = err
			} else {
				c.err = checkVersion(string(output))
			}
			return nil
		})
		if err := wait(); err != nil {
			c.err = err
		}
		if c.err != nil && ctx.Err() == nil && warning != nil {
			warning(c.err)
		}
	})
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return c.err == nil, nil
}

var ruffVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func checkVersion(output string) error {
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != "ruff" {
		return fmt.Errorf("could not identify Ruff version; requires 0.17.0 or newer: %q", strings.TrimSpace(output))
	}
	match := ruffVersionPattern.FindStringSubmatch(fields[1])
	if match != nil {
		for index, minimum := range []uint64{0, 17, 0} {
			value, err := strconv.ParseUint(match[index+1], 10, 64)
			if err != nil || value < minimum {
				break
			}
			if value > minimum || index == 2 && match[4] == "" {
				return nil
			}
		}
	}
	return fmt.Errorf("ruff %q is incompatible; requires 0.17.0 or newer", fields[1])
}
