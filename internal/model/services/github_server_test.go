package services

import "testing"

func TestGitHubExecutionServerService(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url  string
		kind Kind
	}{
		{"", Unknown},
		{"https://github.com", GitHubCom},
		{"https://GITHUB.COM/", GitHubCom},
		{"https://company.ghe.com", GitHubDataResidency},
		{"https://company.github.us", Unknown},
		{"https://github.company.example", GitHubEnterpriseServer},
		{"https://github.com.company.example", GitHubEnterpriseServer},
	} {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			service, err := FromGitHubServerURL(tc.url)
			if err != nil || service.Kind() != tc.kind || service.Version() != "" {
				t.Fatalf("execution service = %v, error = %v; want %s with unknown version", service, err, tc.kind)
			}
		})
	}
	for _, raw := range []string{"github.com", "https://github.com/owner/repo", "file:///github.com", "https://user:secret@github.com", "https://github.com?service=other"} {
		if _, err := FromGitHubServerURL(raw); err == nil {
			t.Fatalf("accepted invalid execution server %q", raw)
		}
	}
}
