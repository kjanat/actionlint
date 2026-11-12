package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

const (
	maxListing          = 16 << 20
	advertisementType   = "application/x-git-upload-pack-advertisement"
	serviceAnnouncement = "# service=git-upload-pack\n"
	emptyRepositoryRef  = "0000000000000000000000000000000000000000 capabilities^{}"
)

var (
	errEnvironment             = errors.New("GitHub Git or raw file host is unavailable")
	errUnsupportedObjectIDs    = errors.New("repository does not use SHA-1 object IDs")
	errInvalidRefAdvertisement = errors.New("invalid Git reference advertisement")
)

type refListing struct {
	repository string
	observedAt string
	digest     string
	refs       map[string]string
	objects    map[string]repository.Object
	peeled     map[string]string
}

func splitRepository(name string) (string, string, bool) {
	owner, repo, ok := strings.Cut(name, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") || strings.ContainsAny(name, "?#%\\\x00\r\n") || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", "", false
	}
	return owner, repo, true
}

func (g *GitHub) fetchRefListing(ctx context.Context, name string) (*refListing, *availability.Unavailability, error) {
	owner, repo, ok := splitRepository(name)
	if !ok {
		reason := availability.Failed()
		return nil, &reason, nil
	}
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: "resolver", Priority: accounting.Enrichment, RequestKind: accounting.Git})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+".git/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return nil, nil, errors.New("invalid GitHub reference listing request")
	}
	request.Header.Set("User-Agent", "actionlint")
	if g.token != "" {
		request.SetBasicAuth("x-access-token", g.token)
	}
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	response, err := g.exchange(ctx, request, maxListing)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("%w: reference listing request failed: %w", errEnvironment, err)
	}
	if response.status != http.StatusOK {
		return nil, unavailableStatus(response.status), environmentStatus(response.status)
	}
	if mediaType(response.header) != advertisementType || len(response.body) > maxListing {
		return nil, nil, fmt.Errorf("%w: unexpected reference listing response", errEnvironment)
	}
	listing, err := parseRefAdvertisement(response.body)
	if errors.Is(err, errUnsupportedObjectIDs) {
		reason := availability.UnsupportedObjectFormat()
		return nil, &reason, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errEnvironment, err)
	}
	listing.repository, listing.observedAt = name, observedAt
	return listing, nil, nil
}

func mediaType(header http.Header) string {
	if value, _, err := mime.ParseMediaType(header.Get("Content-Type")); err == nil {
		return value
	}
	return ""
}

func environmentStatus(status int) error {
	if status >= 500 || status == http.StatusTooManyRequests || status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("%w: status %d", errEnvironment, status)
	}
	return nil
}

func unavailableStatus(status int) *availability.Unavailability {
	if environmentStatus(status) != nil {
		return nil
	}
	reason := availability.Failed()
	if status == http.StatusNotFound {
		reason = availability.Inaccessible()
	}
	return &reason
}

type pktLines struct{ data []byte }

// Packet format: https://git-scm.com/docs/protocol-common#_pkt_line_format
func (p *pktLines) next() ([]byte, bool, error) {
	if len(p.data) < 4 {
		return nil, false, fmt.Errorf("%w: truncated packet", errInvalidRefAdvertisement)
	}
	header, err := hex.DecodeString(string(p.data[:4]))
	if err != nil {
		return nil, false, fmt.Errorf("%w: invalid packet length", errInvalidRefAdvertisement)
	}
	length := int(header[0])<<8 | int(header[1])
	if length == 0 {
		p.data = p.data[4:]
		return nil, true, nil
	}
	if length < 4 || length > 65520 || length > len(p.data) {
		return nil, false, fmt.Errorf("%w: invalid packet length", errInvalidRefAdvertisement)
	}
	payload := p.data[4:length]
	p.data = p.data[length:]
	return payload, false, nil
}

func lowerSHA1(id string) bool {
	return validSHA(id) && id == strings.ToLower(id)
}

// Protocol v0 advertisement: https://git-scm.com/docs/http-protocol#_smart_clients
func parseRefAdvertisement(body []byte) (*refListing, error) {
	lines := pktLines{data: body}
	announcement, flush, err := lines.next()
	if err != nil {
		return nil, err
	}
	if flush || string(announcement) != serviceAnnouncement {
		return nil, fmt.Errorf("%w: missing service announcement", errInvalidRefAdvertisement)
	}
	if _, flush, err = lines.next(); err != nil || !flush {
		return nil, fmt.Errorf("%w: missing flush after service announcement", errInvalidRefAdvertisement)
	}
	refs := make(map[string]string)
	peeled := make(map[string]string)
	seen := make(map[string]bool)
	first, empty := true, false
	previousName, previousID := "", ""
	for {
		payload, flush, err := lines.next()
		if err != nil {
			return nil, err
		}
		if flush {
			break
		}
		line := strings.TrimSuffix(string(payload), "\n")
		if first {
			first = false
			advertised, capabilities, found := strings.Cut(line, "\x00")
			if !found {
				return nil, fmt.Errorf("%w: missing capabilities", errInvalidRefAdvertisement)
			}
			for capability := range strings.FieldsSeq(capabilities) {
				if format, ok := strings.CutPrefix(capability, "object-format="); ok && format != "sha1" {
					return nil, errUnsupportedObjectIDs
				}
			}
			if advertised == emptyRepositoryRef {
				empty = true
				continue
			}
			line = advertised
		}
		if empty || strings.Contains(line, "\x00") {
			return nil, fmt.Errorf("%w: unexpected reference line", errInvalidRefAdvertisement)
		}
		id, name, found := strings.Cut(line, " ")
		if !found || !lowerSHA1(id) || name == "" {
			return nil, fmt.Errorf("%w: invalid reference line", errInvalidRefAdvertisement)
		}
		if base, isPeeled := strings.CutSuffix(name, "^{}"); isPeeled {
			if base != previousName || previousName == "" {
				return nil, fmt.Errorf("%w: peeled reference without its tag", errInvalidRefAdvertisement)
			}
			if prior, exists := peeled[previousID]; exists && prior != id {
				return nil, fmt.Errorf("%w: one tag peels to two objects", errInvalidRefAdvertisement)
			}
			peeled[previousID] = id
			previousName = ""
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: duplicate reference", errInvalidRefAdvertisement)
		}
		seen[name] = true
		previousName, previousID = name, id
		if name == "HEAD" || validRef(name) {
			refs[name] = id
		}
	}
	if len(lines.data) != 0 {
		return nil, fmt.Errorf("%w: data after the final flush", errInvalidRefAdvertisement)
	}
	objects := make(map[string]repository.Object, len(refs)+len(peeled))
	for name, id := range refs {
		if name == "HEAD" || strings.HasPrefix(name, "refs/heads/") {
			objects[id] = repository.Commit{}
		}
	}
	for tag, commit := range peeled {
		if _, isTag := peeled[commit]; isTag {
			return nil, fmt.Errorf("%w: peeled object is a tag", errInvalidRefAdvertisement)
		}
		if _, known := objects[commit]; known {
			objects[tag] = repository.PeeledTag{Commit: commit}
		}
	}
	sum := sha256.Sum256(body)
	return &refListing{refs: refs, objects: objects, peeled: peeled, digest: hex.EncodeToString(sum[:])}, nil
}

type refListings struct {
	mu      sync.Mutex
	entries map[string]*refListingEntry
}

type refListingEntry struct {
	ready     chan struct{}
	abandoned bool
	listing   *refListing
	missing   *availability.Unavailability
	err       error
}

func newRefListings() *refListings {
	return &refListings{entries: make(map[string]*refListingEntry)}
}

func (l *refListings) get(ctx context.Context, g *GitHub, name string) (*refListing, *availability.Unavailability, error) {
	key := strings.ToLower(name)
	for {
		l.mu.Lock()
		entry, exists := l.entries[key]
		if !exists {
			entry = &refListingEntry{ready: make(chan struct{})}
			l.entries[key] = entry
			l.mu.Unlock()
			listing, missing, err := g.fetchRefListing(ctx, name)
			l.mu.Lock()
			if err != nil && !errors.Is(err, errEnvironment) {
				entry.abandoned = true
				delete(l.entries, key)
			}
			entry.listing, entry.missing, entry.err = listing, missing, err
			close(entry.ready)
			l.mu.Unlock()
			return listing, missing, err
		}
		l.mu.Unlock()
		if err := references.WaitOutsideLookupBudget(ctx, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-entry.ready:
				return nil
			}
		}); err != nil {
			return nil, nil, err
		}
		if !entry.abandoned {
			return entry.listing, entry.missing, entry.err
		}
	}
}

type refListingKey struct{}

func withRefListing(ctx context.Context, listing *refListing) context.Context {
	return context.WithValue(ctx, refListingKey{}, listing)
}

func refListingFromContext(ctx context.Context) *refListing {
	listing, _ := ctx.Value(refListingKey{}).(*refListing)
	return listing
}

type listingSession struct {
	listing  *refListing
	name     string
	log      []string
	finished bool
	github   *GitHub
	request  references.Request
	verified repository.Session
}

func newListingSession(github *GitHub, listing *refListing, request references.Request) *listingSession {
	return &listingSession{github: github, request: request, listing: listing, name: request.Repository, log: []string{"listing sha256:" + listing.digest}}
}

func (s *listingSession) Repository() repository.Identity {
	return repository.Identity{Host: "github.com", Name: s.name, ObservedAt: s.listing.observedAt}
}

func (s *listingSession) Reference(ctx context.Context, name string) (string, bool, error) {
	if s.finished {
		return "", false, errors.New("repository session is finished")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if name != "HEAD" && !validRef(name) {
		return "", false, errors.New("invalid exact Git reference")
	}
	id, found := s.listing.refs[name]
	s.log = append(s.log, "reference "+name)
	return id, found, nil
}

func (s *listingSession) Object(ctx context.Context, id string) (repository.Object, bool, error) {
	if s.finished {
		return nil, false, errors.New("repository session is finished")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if len(id) == 64 {
		return nil, false, errors.New("repository object access does not support SHA-256 object IDs")
	}
	if !validSHA(id) {
		return nil, false, errors.New("invalid Git object ID")
	}
	id = strings.ToLower(id)
	object, found := s.listing.objects[id]
	s.log = append(s.log, "object "+id)
	if found {
		return object, true, nil
	}
	if s.verified == nil {
		ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: "resolver", Priority: accounting.Enrichment, RequestKind: accounting.REST, QuotaPool: "core"})
		request, err := s.github.request(ctx, "https://api.github.com/repos/"+s.name, false)
		if err != nil {
			return nil, false, err
		}
		if tracker := accounting.RequestTrackerFromContext(ctx); tracker != nil {
			if err := tracker.LoadGitHubRateLimits(ctx, s.github.client, request); err != nil {
				return nil, false, err
			}
			if err := tracker.CheckBatchQuota(request, 1); err != nil {
				return nil, false, err
			}
		}
		session, err := s.github.Open(withRefListing(ctx, nil), s.request)
		if err != nil {
			return nil, false, err
		}
		s.verified = session
	}
	return s.verified.Object(ctx, id)
}

func (s *listingSession) Finish(ctx context.Context) (json.RawMessage, error) {
	if s.finished {
		return nil, errors.New("repository session is finished")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.finished = true
	if s.verified != nil {
		log, err := s.verified.Finish(ctx)
		if err != nil {
			return nil, err
		}
		s.log = append(s.log, string(log))
	}
	return json.Marshal(struct {
		Host         string   `json:"host"`
		Observations []string `json:"observations"`
	}{Host: "github.com", Observations: s.log})
}
