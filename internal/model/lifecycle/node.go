package lifecycle

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/services"
)

// Update the Node.js schedule with go run ./internal/development/generators/node-releases -revision COMMIT.

//go:embed data/node-schedule.json
var nodeScheduleJSON []byte

//go:embed data/node-source.txt
var nodeScheduleSource string

type nodeRelease struct{ start, end Date }

var nodeReleases = readNodeReleases()

func readNodeReleases() map[string]nodeRelease {
	var records map[string]struct{ Start, End string }
	if err := json.Unmarshal(nodeScheduleJSON, &records); err != nil {
		panic(err)
	}
	releases := make(map[string]nodeRelease, len(records))
	for version, record := range records {
		start, startErr := ParseDate(record.Start)
		end, endErr := ParseDate(record.End)
		if startErr != nil || endErr != nil || start.Compare(end) >= 0 {
			panic("invalid embedded Node.js schedule for " + version)
		}
		releases[strings.TrimPrefix(version, "v")] = nodeRelease{start, end}
	}
	return releases
}

var nodeSelector = regexp.MustCompile(`^v?(0|[1-9][0-9]*)(?:\.(?:[xX*]|(?:0|[1-9][0-9]*)(?:\.(?:[xX*]|0|[1-9][0-9]*))?))?$`)

// NodeLifecycle returns support evidence only for selectors confined to one known major.
func NodeLifecycle(selector string, asOf Date, service services.Service) (Evidence, string, bool) {
	match := nodeSelector.FindStringSubmatch(selector)
	if match == nil || asOf.IsZero() || match[1] == "0" {
		return Evidence{}, "", false
	}
	release, ok := nodeReleases[match[1]]
	if !ok {
		return Evidence{}, "", false
	}
	evidence := Evidence{
		Subject: LanguageRuntimes, Name: "node" + match[1], Kind: SupportEnds,
		Service: service, Effective: release.end, AsOf: asOf,
		Source: strings.TrimSpace(nodeScheduleSource), Current: asOf.Compare(release.end) >= 0,
	}
	if asOf.Compare(release.start) < 0 {
		evidence.Kind, evidence.Effective, evidence.Current = SupportStarts, release.start, true
		return evidence, fmt.Sprintf("Node.js %s is scheduled for release on %s and is not yet available.", match[1], release.start), true
	}
	message := fmt.Sprintf("Support for Node.js %s ends on %s.", match[1], release.end)
	if evidence.Current {
		message = fmt.Sprintf("Support for Node.js %s ended on %s.", match[1], release.end)
	}
	return evidence, message, true
}
