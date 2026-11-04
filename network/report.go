// Package network accounts for invocation-owned network operations and admits
// requests against observed quotas. Reports contain no URLs or credentials.
package network

import "time"

// Kind separates HTTP API requests, artifact traffic and Git operations.
type Kind string

const (
	HTTP      Kind = "http"
	REST      Kind = "rest"
	GraphQL   Kind = "graphql"
	Git       Kind = "git"
	OCI       Kind = "oci"
	Delegated Kind = "delegated"
)

// Authentication describes credentials supplied, not proof of an authenticated
// identity. External processes have unknown authentication without a receipt.
type Authentication string

const (
	Anonymous             Authentication = "no"
	Credentialed          Authentication = "yes"
	UnknownAuthentication Authentication = "unknown"
)

// Priority distinguishes dependency acquisition, optional enrichment and quota
// control traffic. Priority labels alone do not reserve future request units.
type Priority string

const (
	Required   Priority = "required"
	Enrichment Priority = "enrichment"
	Control    Priority = "control"
)

// Operation identifies why a request is needed. Cost is an upper bound in the
// server's quota units; zero means unknown. REST requests default to one unit.
type Operation struct {
	// Purpose is a fixed label such as plugin-registry, never a URL or ref value.
	Purpose  string
	Priority Priority
	Kind     Kind
	Resource string
	Cost     uint64
}

// Group combines operations with the same host, credential status, protocol and purpose.
// Attempts counts transport calls, including failures before a response arrives.
// Blocked counts rejected requests or batch checks, never transport attempts.
// Opaque lookups are not HTTP counts.
type Group struct {
	Lookups          uint64         `json:"lookups"`
	Host             string         `json:"host"`
	Authentication   Authentication `json:"authenticated"`
	Kind             Kind           `json:"kind"`
	Purpose          string         `json:"purpose"`
	Priority         Priority       `json:"priority"`
	Attempts         uint64         `json:"attempts"`
	Failures         uint64         `json:"failures"`
	Blocked          uint64         `json:"blocked"`
	CacheHits        uint64         `json:"cache_hits"`
	OpaqueLookups    uint64         `json:"opaque_lookups"`
	ProviderReported uint64         `json:"provider_reported"`
}

// Quota is the latest conservative observation for a host and credential scope.
// Independent credentials can produce separate rows with identical public labels.
// No credential fingerprint is published.
type Quota struct {
	Host           string         `json:"host"`
	Authentication Authentication `json:"authenticated"`
	Resource       string         `json:"resource"`
	Remaining      *uint64        `json:"remaining"`
	Reserved       uint64         `json:"reserved"`
	Reset          *time.Time     `json:"reset"`
	RetryAfter     *time.Time     `json:"retry_after,omitempty"`
}

type Report struct {
	Groups []Group `json:"groups"`
	Quotas []Quota `json:"quotas"`
}
