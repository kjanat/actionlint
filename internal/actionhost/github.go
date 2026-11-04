// Package actionhost acquires action manifests under an explicit host network
// policy. The semantic action packages never select an HTTP client or cache.
package actionhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/knowledge"
	"actionlint.kjanat.dev/v2/network"
	"actionlint.kjanat.dev/v2/resolver"
)

type Mode string

const (
	Off     Mode = "off"
	Offline Mode = "offline"
	Online  Mode = "online"
)

// Options selects acquisition explicitly. Tokens are never cached or included
// in returned failures. A supplied resolver must retain action selection routes.
type Options struct {
	Mode      Mode
	CacheDir  string
	Token     string
	Resolver  func() resolver.Resolver
	Transport http.RoundTripper
}

type GitHub struct {
	mode            Mode
	cacheDir, token string
	resolver        func() resolver.Resolver
	client          *http.Client
}

const (
	maxBody  = 1 << 20
	maxCache = 8 << 20
)

func New(options Options) (*GitHub, error) {
	if options.Mode == "" {
		options.Mode = Off
	}
	if options.Mode != Off && options.Mode != Offline && options.Mode != Online {
		return nil, errors.New("action metadata mode must be off, offline or online")
	}
	if strings.ContainsAny(options.Token, "\r\n\x00") {
		return nil, errors.New("invalid GitHub credential header")
	}
	return &GitHub{mode: options.Mode, cacheDir: options.CacheDir, token: options.Token, resolver: options.Resolver, client: &http.Client{
		Transport: network.Transport(options.Transport), Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func fullSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fullSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Lookup natively acquires only full SHA-1 commit requests. Named refs require
// an injected action-profile resolver; the commits API never selects their ref.
func (g *GitHub) Lookup(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return actions.Manifest{}, err
	}
	if g.mode == Off {
		return actions.UnavailableManifest(knowledge.LookupDisabled()), nil
	}
	if ref.Host() != "github.com" {
		return actions.UnavailableManifest(knowledge.Unsupported()), nil
	}
	if g.mode == Offline {
		if !fullSHA1(ref.Ref()) {
			return actions.UnavailableManifest(knowledge.Offline()), nil
		}
		return g.offline(ctx, ref)
	}
	if !fullSHA1(ref.Ref()) && (fullSHA256(ref.Ref()) || g.resolver == nil) {
		return actions.UnavailableManifest(knowledge.Unsupported()), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = network.WithOperation(ctx, network.Operation{Purpose: "action-metadata", Priority: network.Enrichment, Kind: network.REST, Resource: "core"})
	if session := network.FromContext(ctx); session != nil {
		request, err := g.request(ctx, repositoryURL(ref), false)
		if err != nil {
			return actions.Manifest{}, err
		}
		if err := session.PreflightGitHub(ctx, g.client, request); err != nil {
			return actions.Manifest{}, err
		}
		if err := session.CheckBudget(request, 1); err != nil {
			return actions.UnavailableManifest(knowledge.Limit()), nil
		}
	}
	repo, missing, err := g.repository(ctx, ref)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	commit := strings.ToLower(ref.Ref())
	object := commit
	route := actions.ObjectRoute()
	observed := time.Now().UTC().Format(time.RFC3339Nano)
	if !fullSHA1(ref.Ref()) {
		resolved, resolveErr := g.resolver().Resolve(ctx, resolver.Request{Version: 1, Repository: ref.Repository(), Ref: ref.Ref(), Kind: "action", ExpectedRepositoryID: repo.ID})
		if resolveErr != nil {
			return unavailableError(resolveErr)
		}
		if resolved.Version != 1 || resolved.Error != "" || resolved.RepositoryID != repo.ID || !strings.EqualFold(resolved.Repository, ref.Repository()) || !fullSHA1(resolved.Commit) || resolved.Tree != "" || resolved.ObservedAt == "" || len(resolved.Evidence) == 0 || !fullSHA1(resolved.SelectedObject) {
			return actions.UnavailableManifest(knowledge.Unsupported()), nil
		}
		route, err = actions.RefRoute(resolved.SelectedRef)
		if err != nil {
			return actions.UnavailableManifest(knowledge.Unsupported()), nil
		}
		commit, object, observed = resolved.Commit, resolved.SelectedObject, resolved.ObservedAt
	}
	identity, err := actions.NewIdentity(ref.Host(), repo.Name, repo.ID, commit, ref.Directory())
	if err != nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	selection, err := actions.NewSelection(ref, identity, object, route, observed)
	if err != nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	if cached, exists, cacheErr := g.cached(ctx, ref, repo.ID, commit); cacheErr == nil && exists {
		return actions.NewManifestFile(selection, cached.Filename, []byte(cached.Content))
	}
	if session := network.FromContext(ctx); session != nil {
		request, err := g.request(ctx, repositoryURL(ref), false)
		if err != nil {
			return actions.Manifest{}, err
		}
		if err := session.CheckBudget(request, 3); err != nil {
			return actions.UnavailableManifest(knowledge.Limit()), nil
		}
	}
	var objectResponse struct {
		SHA string `json:"sha"`
	}
	objectBytes, missing, err := g.get(ctx, repositoryURL(ref)+"/git/commits/"+commit, false)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if json.Unmarshal(objectBytes, &objectResponse) != nil || !strings.EqualFold(objectResponse.SHA, commit) {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	filename := "action.yml"
	content, missing, err := g.get(ctx, manifestURL(ref, commit, filename), true)
	if err != nil {
		return actions.Manifest{}, err
	}
	if missing != nil && missing.Reason() == knowledge.InaccessibleReason {
		filename = "action.yaml"
		content, missing, err = g.get(ctx, manifestURL(ref, commit, filename), true)
	}
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	// Recheck the repository guard after name-addressed acquisition. This is an
	// observation, not a proof of uninterrupted ownership or runner execution.
	after, missing, err := g.repository(ctx, ref)
	if err != nil || missing != nil {
		return unavailable(missing, err)
	}
	if after.ID != repo.ID || !strings.EqualFold(after.Name, repo.Name) {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	manifest, err := actions.NewManifestFile(selection, filename, content)
	if err != nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	if err := g.store(ctx, manifest); err != nil {
		if ctx.Err() != nil {
			return actions.Manifest{}, ctx.Err()
		}
		// The network observation is usable even when the user's cache cannot be written.
	}
	return manifest, nil
}

type repositoryIdentity struct {
	ID   uint64 `json:"id"`
	Name string `json:"full_name"`
}

func (g *GitHub) repository(ctx context.Context, ref actions.Reference) (repositoryIdentity, *knowledge.Unavailability, error) {
	var repo repositoryIdentity
	content, missing, err := g.get(ctx, repositoryURL(ref), false)
	if err == nil && missing == nil && (json.Unmarshal(content, &repo) != nil || repo.ID == 0 || !strings.EqualFold(repo.Name, ref.Repository())) {
		reason := knowledge.Failed()
		missing = &reason
	}
	return repo, missing, err
}

func repositoryURL(ref actions.Reference) string {
	parts := strings.Split(ref.Repository(), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "https://api.github.com/repos/" + strings.Join(parts, "/")
}

func manifestURL(ref actions.Reference, commit, filename string) string {
	var parts []string
	if ref.Directory() != "" {
		parts = strings.Split(ref.Directory(), "/")
	}
	parts = append(parts, filename)
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return repositoryURL(ref) + "/contents/" + strings.Join(parts, "/") + "?ref=" + url.QueryEscape(commit)
}

func (g *GitHub) request(ctx context.Context, address string, raw bool) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("invalid GitHub metadata request")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if raw {
		request.Header.Set("Accept", "application/vnd.github.raw+json")
	}
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "actionlint")
	if g.token != "" {
		request.Header.Set("Authorization", "Bearer "+g.token)
	}
	return request, nil
}

func (g *GitHub) get(ctx context.Context, address string, raw bool) ([]byte, *knowledge.Unavailability, error) {
	request, err := g.request(ctx, address, raw)
	if err != nil {
		return nil, nil, err
	}
	response, err := g.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		reason := knowledge.Failed()
		if errors.Is(err, network.ErrRateLimited) || errors.Is(err, network.ErrBudget) {
			reason = knowledge.Limit()
		}
		return nil, &reason, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		reason := knowledge.Failed()
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			reason = knowledge.Denied()
			if response.StatusCode == http.StatusForbidden && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
				reason = knowledge.Limit()
			}
		case http.StatusNotFound:
			reason = knowledge.Inaccessible()
		case http.StatusTooManyRequests:
			reason = knowledge.Limit()
		}
		return nil, &reason, nil
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		reason := knowledge.Failed()
		return nil, &reason, nil
	}
	if len(content) > maxBody {
		reason := knowledge.Limit()
		return nil, &reason, nil
	}
	return content, nil, nil
}

func unavailable(missing *knowledge.Unavailability, err error) (actions.Manifest, error) {
	if err != nil {
		return actions.Manifest{}, err
	}
	if missing == nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	return actions.UnavailableManifest(*missing), nil
}

func unavailableError(err error) (actions.Manifest, error) {
	if errors.Is(err, context.Canceled) {
		return actions.Manifest{}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return actions.Manifest{}, context.DeadlineExceeded
	}
	return actions.UnavailableManifest(knowledge.Failed()), nil
}

type cacheRecord struct {
	Version      int    `json:"version"`
	Host         string `json:"host"`
	Repository   string `json:"repository"`
	RepositoryID uint64 `json:"repository_id"`
	Commit       string `json:"commit"`
	Directory    string `json:"directory"`
	ObservedAt   string `json:"observed_at"`
	Filename     string `json:"filename"`
	Content      string `json:"content"`
	SHA256       string `json:"sha256"`
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (g *GitHub) bucket(ref actions.Reference, commit string) string {
	return filepath.Join(g.cacheDir, digest(ref.Host()+"\x00"+strings.ToLower(ref.Repository())+"\x00"+strings.ToLower(commit)+"\x00"+ref.Directory()))
}

func (g *GitHub) cached(ctx context.Context, ref actions.Reference, id uint64, commit string) (cacheRecord, bool, error) {
	if g.cacheDir == "" {
		return cacheRecord{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return cacheRecord{}, false, err
	}
	reader := fsread.New(ctx, nil, nil)
	file, err := reader.Open(filepath.Join(g.bucket(ref, commit), strconv.FormatUint(id, 10)+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return cacheRecord{}, false, nil
	}
	if err != nil {
		return cacheRecord{}, false, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxCache+1))
	if err != nil {
		return cacheRecord{}, false, err
	}
	var record cacheRecord
	if len(content) > maxCache || json.Unmarshal(content, &record) != nil || record.Version != 1 || record.Host != ref.Host() || !strings.EqualFold(record.Repository, ref.Repository()) || record.RepositoryID != id || !strings.EqualFold(record.Commit, commit) || record.Directory != ref.Directory() || record.SHA256 != digest(record.Content) || len(record.Content) > maxBody || (record.Filename != "action.yml" && record.Filename != "action.yaml") {
		return cacheRecord{}, false, errors.New("invalid action metadata cache record")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.ObservedAt); err != nil {
		return cacheRecord{}, false, errors.New("invalid action metadata cache observation")
	}
	if err := ctx.Err(); err != nil {
		return cacheRecord{}, false, err
	}
	return record, true, nil
}

func (g *GitHub) offline(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
	if g.cacheDir == "" {
		return actions.UnavailableManifest(knowledge.CacheMiss()), nil
	}
	reader := fsread.New(ctx, nil, nil)
	bucket := g.bucket(ref, ref.Ref())
	directory, err := reader.Open(bucket)
	if errors.Is(err, os.ErrNotExist) {
		return actions.UnavailableManifest(knowledge.CacheMiss()), nil
	}
	if err != nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	defer directory.Close()
	// A bucket contains one repository-ID record. Bound enumeration too;
	// ambiguous identities or excessive debris must not choose an arbitrary hit.
	entries, err := directory.ReadDir(3)
	if err != nil && !errors.Is(err, io.EOF) {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	if err := ctx.Err(); err != nil {
		return actions.Manifest{}, err
	}
	if len(entries) == 3 {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	var id uint64
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() {
			continue
		}
		parsed, err := strconv.ParseUint(name, 10, 64)
		if err != nil || parsed == 0 {
			continue
		}
		if id != 0 {
			return actions.UnavailableManifest(knowledge.Failed()), nil
		}
		id = parsed
	}
	if id == 0 {
		return actions.UnavailableManifest(knowledge.CacheMiss()), nil
	}
	record, exists, err := g.cached(ctx, ref, id, ref.Ref())
	if err != nil {
		if ctx.Err() != nil {
			return actions.Manifest{}, ctx.Err()
		}
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	if !exists {
		return actions.UnavailableManifest(knowledge.CacheMiss()), nil
	}
	identity, err := actions.NewIdentity(record.Host, record.Repository, id, record.Commit, record.Directory)
	if err != nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	selection, err := actions.NewSelection(ref, identity, strings.ToLower(ref.Ref()), actions.ObjectRoute(), record.ObservedAt)
	if err != nil {
		return actions.UnavailableManifest(knowledge.Failed()), nil
	}
	return actions.NewManifestFile(selection, record.Filename, []byte(record.Content))
}

func (g *GitHub) store(ctx context.Context, manifest actions.Manifest) error {
	if g.cacheDir == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i := manifest.Identity()
	s := manifest.Selection()
	record := cacheRecord{Version: 1, Host: i.Host(), Repository: i.Repository(), RepositoryID: i.RepositoryID(), Commit: i.Commit(), Directory: i.Directory(), ObservedAt: s.ObservedAt(), Filename: manifest.Filename(), Content: manifest.Content(), SHA256: digest(manifest.Content())}
	content, err := json.Marshal(record)
	if err != nil {
		return err
	}
	bucket := g.bucket(s.Request(), i.Commit())
	if err := os.MkdirAll(bucket, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(bucket, ".metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(bucket, strconv.FormatUint(i.RepositoryID(), 10)+".json"))
}
