// Package accounting accounts for invocation-owned network operations and admits
// requests against observed quotas. Reports contain no URLs or credentials.
package accounting

import "time"

// RequestKind separates HTTP API requests, artifact traffic and Git operations.
type RequestKind string

const (
	HTTP      RequestKind = "http"
	REST      RequestKind = "rest"
	GraphQL   RequestKind = "graphql"
	Git       RequestKind = "git"
	OCI       RequestKind = "oci"
	Delegated RequestKind = "delegated"
)

// CredentialStatus describes credentials supplied, not proof of an authenticated
// identity. External processes have unknown credential status without a request report.
type CredentialStatus string

const (
	NoCredentials       CredentialStatus = "absent"
	CredentialsProvided CredentialStatus = "provided"
	CredentialsUnknown  CredentialStatus = "unknown"
)

// Priority distinguishes dependency acquisition, optional enrichment and quota
// control traffic. Priority labels alone do not reserve future request units.
type Priority string

const (
	Required   Priority = "required"
	Enrichment Priority = "enrichment"
	Control    Priority = "control"
)

// RequestOptions identifies why a request is needed. QuotaCost is an upper bound in the
// server's quota units; zero means unknown. REST requests default to one unit.
type RequestOptions struct {
	// Purpose is a fixed label such as plugin-registry, never a URL or ref value.
	Purpose     string
	Priority    Priority
	RequestKind RequestKind
	QuotaPool   string
	QuotaCost   uint64
}

// RequestCounts combines operations with the same host, credential status, protocol and purpose.
// Attempts counts transport calls, including failures before a response arrives.
// QuotaRejections counts rejected requests or batch checks, never transport attempts.
// UnreportedResolverCalls counts resolver calls without a complete request report.
type RequestCounts struct {
	ResolverCalls            uint64           `json:"resolver_calls"`
	Host                     string           `json:"host"`
	Credentials              CredentialStatus `json:"credentials"`
	RequestKind              RequestKind      `json:"request_kind"`
	Purpose                  string           `json:"purpose"`
	Priority                 Priority         `json:"priority"`
	Attempts                 uint64           `json:"attempts"`
	Failures                 uint64           `json:"failures"`
	QuotaRejections          uint64           `json:"quota_rejections"`
	CacheHits                uint64           `json:"cache_hits"`
	UnreportedResolverCalls  uint64           `json:"unreported_resolver_calls"`
	ResolverReportedAttempts uint64           `json:"resolver_reported_attempts"`
}

// QuotaStatus is the latest conservative observation for a host and credential scope.
// Independent credentials can produce separate rows with identical public labels.
// No credential fingerprint is published.
type QuotaStatus struct {
	Host        string           `json:"host"`
	Credentials CredentialStatus `json:"credentials"`
	Pool        string           `json:"pool"`
	Remaining   *uint64          `json:"remaining"`
	Reserved    uint64           `json:"reserved"`
	Reset       *time.Time       `json:"reset"`
	RetryAfter  *time.Time       `json:"retry_after,omitempty"`
}

type Statistics struct {
	Requests []RequestCounts `json:"requests"`
	Quotas   []QuotaStatus   `json:"quotas"`
}
