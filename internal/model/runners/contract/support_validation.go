package runner

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/services"
)

var (
	supportID      = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	supportArch    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	supportVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(?:\.[0-9]+)?$`)
)

// ValidateSupportCatalog checks provider evidence before the host stores it.
func ValidateSupportCatalog(c SupportCatalog, scope string) error {
	if len(c.Policies) == 0 && c.DatasetStatus == (DatasetStatus{}) {
		return nil
	}
	if len(c.Policies) > 256 || c.DatasetStatus.DatasetID == "" || c.DatasetStatus.Scope != scope {
		return errors.New("invalid runner support catalogue identity or scope")
	}
	switch c.DatasetStatus.Freshness {
	case Fresh, Stale, Missing:
	default:
		return errors.New("invalid runner support freshness")
	}
	ids := map[string]bool{}
	for _, p := range c.Policies {
		if len(p.ID) > 128 || len(p.Capability) > 128 || !supportID.MatchString(p.ID) || ids[p.ID] || !supportID.MatchString(string(p.Capability)) || p.Description == "" || len(p.Description) > 4096 {
			return fmt.Errorf("invalid runner support policy %q", p.ID)
		}
		ids[p.ID] = true
		u, err := url.Parse(p.URL)
		if err != nil || len(p.URL) > 2048 || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("invalid runner support policy URL for %s", p.ID)
		}
		switch p.Service.Kind {
		case services.Unknown, services.GitHubCom, services.GitHubDataResidency, services.GitHubEnterpriseServer:
		default:
			return errors.New("invalid service scope")
		}
		if p.Service.Kind != services.GitHubEnterpriseServer && (p.Service.MinVersion != "" || p.Service.BeforeVersion != "") {
			return errors.New("version bounds require GHES")
		}
		for _, v := range []string{p.Service.MinVersion, p.Service.BeforeVersion} {
			if v != "" && !supportVersion.MatchString(v) {
				return errors.New("invalid service version")
			}
		}
		if p.Service.MinVersion != "" && p.Service.BeforeVersion != "" && services.CompareVersions(p.Service.MinVersion, p.Service.BeforeVersion) >= 0 {
			return errors.New("invalid GHES version range")
		}
		if len(p.Hosting) > 3 || len(p.OS) > 4 || len(p.Architectures) > 64 || len(p.Changes) > 128 {
			return errors.New("runner support policy exceeds limits")
		}
		for _, h := range p.Hosting {
			if h != Hosted && h != SelfHosted {
				return errors.New("invalid hosting constraint")
			}
		}
		for _, os := range p.OS {
			if os != Linux && os != Windows && os != MacOS {
				return errors.New("invalid OS constraint")
			}
		}
		for _, arch := range p.Architectures {
			if !supportArch.MatchString(NormalizeArchitecture(arch)) {
				return errors.New("invalid architecture constraint")
			}
		}
		if !validSupportState(p.Initial) {
			return errors.New("invalid initial support state")
		}
		var last Date
		for _, change := range p.Changes {
			if change.On.IsZero() || !validSupportState(change.State) || !last.IsZero() && change.On.Compare(last) <= 0 {
				return errors.New("invalid support transition")
			}
			last = change.On
		}
	}
	return nil
}

func validSupportState(s SupportState) bool {
	return s == SupportUnknown || s == Supported || s == Deprecated || s == Unsupported
}

func NormalizeArchitecture(s string) string {
	switch strings.ToLower(s) {
	case "arm", "armv7", "armv7l":
		return "arm32"
	case "aarch64":
		return "arm64"
	case "amd64", "x86_64":
		return "x64"
	default:
		return strings.ToLower(s)
	}
}

func CloneSupportCatalog(c SupportCatalog) SupportCatalog {
	c.Policies = slices.Clone(c.Policies)
	for i := range c.Policies {
		p := &c.Policies[i]
		p.Hosting = slices.Clone(p.Hosting)
		p.OS = slices.Clone(p.OS)
		p.Architectures = slices.Clone(p.Architectures)
		p.Changes = slices.Clone(p.Changes)
	}
	return c
}
