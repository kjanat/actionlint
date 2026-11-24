// Package services identifies the service that executes a workflow.
package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ServiceKind string

const (
	Unknown                ServiceKind = ""
	GitHubCom              ServiceKind = "github.com"
	GitHubDataResidency    ServiceKind = "github-data-residency"
	GitHubEnterpriseServer ServiceKind = "ghes"
)

type Service struct {
	kind    ServiceKind
	version string
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+(?:\.[0-9]+)?$`)

func ParseService(kind, version string) (Service, error) {
	if kind == "unknown" {
		kind = ""
	}
	k := ServiceKind(kind)
	switch k {
	case Unknown, GitHubCom, GitHubDataResidency:
		if version != "" {
			return Service{}, fmt.Errorf("service %q cannot have a version", kind)
		}
	case GitHubEnterpriseServer:
		if version != "" && !versionPattern.MatchString(version) {
			return Service{}, fmt.Errorf("invalid GHES version %q", version)
		}
	default:
		return Service{}, fmt.Errorf("unknown runner service %q", kind)
	}
	return Service{kind: k, version: version}, nil
}
func (s Service) Kind() ServiceKind { return s.kind }
func (s Service) Version() string   { return s.version }
func (s Service) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind    ServiceKind `json:"kind"`
		Version string      `json:"version,omitempty"`
	}{s.kind, s.version})
}

func (s *Service) UnmarshalJSON(data []byte) error {
	var value struct {
		Kind    string `json:"kind"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseService(value.Kind, value.Version)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

func CompareVersions(a, b string) int {
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(left) || i < len(right); i++ {
		x, y := "0", "0"
		if i < len(left) {
			x = strings.TrimLeft(left[i], "0")
			if x == "" {
				x = "0"
			}
		}
		if i < len(right) {
			y = strings.TrimLeft(right[i], "0")
			if y == "" {
				y = "0"
			}
		}
		if len(x) < len(y) {
			return -1
		}
		if len(x) > len(y) {
			return 1
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}
