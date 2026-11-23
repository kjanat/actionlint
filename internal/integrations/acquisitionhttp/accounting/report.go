// Package accounting counts invocation network operations, and its reports contain no URLs or credentials.
package accounting

import "time"

type RequestKind string

const (
	HTTP      RequestKind = "http"
	REST      RequestKind = "rest"
	GraphQL   RequestKind = "graphql"
	Git       RequestKind = "git"
	OCI       RequestKind = "oci"
	Delegated RequestKind = "delegated"
	// Cache lookups count only CacheHits and CacheMisses.
	Cache RequestKind = "cache"
)

// CredentialStatus describes the supplied credentials and proves no authenticated identity.
type CredentialStatus string

const (
	NoCredentials       CredentialStatus = "absent"
	CredentialsProvided CredentialStatus = "provided"
	CredentialsUnknown  CredentialStatus = "unknown"
)

type Priority string

const (
	Required   Priority = "required"
	Enrichment Priority = "enrichment"
	Control    Priority = "control"
)

// RequestOptions treats QuotaCost as an upper bound in server quota units, and zero means unknown.
type RequestOptions struct {
	// Purpose is a fixed label such as plugin-registry and holds no URL or ref.
	Purpose     string
	Priority    Priority
	RequestKind RequestKind
	QuotaPool   string
	QuotaCost   uint64
}

// RequestCounts from an external resolver set only Host, RequestKind, Credentials, Attempts and Failures, and Host holds no credentials, path or query.
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
	CacheMisses              uint64           `json:"cache_misses"`
	UnreportedResolverCalls  uint64           `json:"unreported_resolver_calls"`
	ResolverReportedAttempts uint64           `json:"resolver_reported_attempts"`
}

// RequestReport sets AllRequestsReported only when it includes every remote attempt, failed ones too.
type RequestReport struct {
	AllRequestsReported bool            `json:"all_requests_reported"`
	Requests            []RequestCounts `json:"requests"`
}

// QuotaStatus publishes no credential fingerprint.
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
