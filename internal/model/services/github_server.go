package services

import (
	"fmt"
	"net/url"
	"strings"
)

// FromGitHubServerURL reads an execution host's GITHUB_SERVER_URL value.
// A repository's clone URL does not establish its workflow execution service.
func FromGitHubServerURL(raw string) (Service, error) {
	if raw == "" {
		return Service{}, nil
	}
	server, err := url.Parse(raw)
	if err != nil || server.Hostname() == "" || server.Scheme != "https" && server.Scheme != "http" || server.User != nil || server.RawQuery != "" || server.Fragment != "" || server.Path != "" && server.Path != "/" {
		return Service{}, fmt.Errorf("invalid GitHub execution server URL %q", raw)
	}
	host := strings.ToLower(server.Hostname())
	switch {
	case host == "github.com":
		return Parse(string(GitHubCom), "")
	case strings.HasSuffix(host, ".ghe.com"):
		return Parse(string(GitHubDataResidency), "")
	case host == "github.us" || strings.HasSuffix(host, ".github.us"):
		return Service{}, nil
	default:
		return Parse(string(GitHubEnterpriseServer), "")
	}
}
