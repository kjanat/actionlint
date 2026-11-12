package github

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type resolverRepository struct {
	g             *GitHub
	base          string
	identity      repository.Identity
	defaultBranch string
	refs          map[string]refObservation
	log           []string
	finished      bool
	requests      int
}
type refObservation struct {
	id    string
	found bool
}
type repositoryResponse struct {
	ID            uint64 `json:"id"`
	Name          string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

// Open checks repository identity and read access before resolving references.
func (g *GitHub) Open(ctx context.Context, request references.Request) (repository.Session, error) {
	if g.mode != Online {
		return nil, errors.New("GitHub repository access is unavailable while network access is off or offline")
	}
	owner, name, valid := splitRepository(request.Repository)
	if request.Version != 1 || !valid {
		return nil, errors.New("invalid GitHub repository request")
	}
	if listing := refListingFromContext(ctx); listing != nil && strings.EqualFold(listing.repository, request.Repository) {
		return newListingSession(listing, request), nil
	}
	s := &resolverRepository{g: g, base: "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), refs: make(map[string]refObservation)}
	repo, err := s.readRepository(ctx)
	if err != nil {
		return nil, err
	}
	id, err := repositoryid.Known(repo.ID)
	if err != nil || !strings.EqualFold(repo.Name, request.Repository) || id.Conflicts(request.ExpectedRepositoryID) || repo.DefaultBranch == "" {
		return nil, errors.New("GitHub repository identity or default branch is invalid")
	}
	s.identity = repository.Identity{Host: "github.com", ID: id, Name: repo.Name, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	s.defaultBranch = repo.DefaultBranch
	// A 404 from a Git endpoint is ambiguous unless contents are readable.
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *resolverRepository) Repository() repository.Identity { return s.identity }
func (s *resolverRepository) get(ctx context.Context, suffix string) ([]byte, bool, error) {
	if s.requests >= 64 {
		return nil, false, errors.New("GitHub repository request limit exceeded")
	}
	s.requests++
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: "resolver", Priority: accounting.Enrichment, RequestKind: accounting.REST, QuotaPool: "core"})
	data, missing, err := s.g.get(ctx, s.base+suffix, false)
	if err != nil {
		return nil, false, err
	}
	if missing != nil {
		if missing.Reason() == availability.InaccessibleReason {
			s.log = append(s.log, suffix+": 404")
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("GitHub repository request unavailable: %s", missing.Reason())
	}
	sum := sha256.Sum256(data)
	s.log = append(s.log, fmt.Sprintf("%s: sha256:%x", suffix, sum))
	return data, true, nil
}

func (s *resolverRepository) readRepository(ctx context.Context) (repositoryResponse, error) {
	data, ok, err := s.get(ctx, "")
	if err != nil {
		return repositoryResponse{}, err
	}
	if !ok {
		return repositoryResponse{}, errors.New("GitHub repository is inaccessible")
	}
	var repo repositoryResponse
	if json.Unmarshal(data, &repo) != nil {
		return repo, errors.New("invalid GitHub repository response")
	}
	return repo, nil
}

func (s *resolverRepository) guard(ctx context.Context) error {
	_, ok, err := s.get(ctx, "/contents?ref="+url.QueryEscape(s.defaultBranch))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("GitHub repository contents are inaccessible")
	}
	return nil
}

func validRef(name string) bool {
	if !strings.HasPrefix(name, "refs/") {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	if strings.HasSuffix(name, "/") || strings.Contains(name, "//") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.ContainsAny(name, " ~^:?*[\\\x00\r\n") {
		return false
	}
	for p := range strings.SplitSeq(name, "/") {
		if p == "" || strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".") || strings.HasSuffix(p, ".lock") {
			return false
		}
	}
	return true
}

func (s *resolverRepository) readReference(ctx context.Context, name string) (refObservation, error) {
	lookup := name
	if name == "HEAD" {
		lookup = "refs/heads/" + s.defaultBranch
	}
	if !validRef(lookup) {
		return refObservation{}, errors.New("invalid exact Git reference")
	}
	segments := strings.Split(strings.TrimPrefix(lookup, "refs/"), "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	data, ok, err := s.get(ctx, "/git/ref/"+strings.Join(segments, "/"))
	if err != nil {
		return refObservation{}, err
	}
	if !ok {
		if err := s.guard(ctx); err != nil {
			return refObservation{}, err
		}
		return refObservation{}, nil
	}
	var response struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if json.Unmarshal(data, &response) != nil || response.Ref != lookup || !validSHA(response.Object.SHA) || (response.Object.Type != "commit" && response.Object.Type != "tag" && response.Object.Type != "tree" && response.Object.Type != "blob") {
		return refObservation{}, errors.New("invalid GitHub reference response")
	}
	return refObservation{id: strings.ToLower(response.Object.SHA), found: true}, nil
}

func (s *resolverRepository) Reference(ctx context.Context, name string) (string, bool, error) {
	if s.finished {
		return "", false, errors.New("repository session is finished")
	}
	observation, err := s.readReference(ctx, name)
	if err != nil {
		return "", false, err
	}
	if prior, ok := s.refs[name]; ok && prior != observation {
		return "", false, errors.New("git reference changed during resolution")
	}
	s.refs[name] = observation
	s.log = append(s.log, "reference "+name)
	return observation.id, observation.found, nil
}

func validSHA(id string) bool {
	if len(id) != 40 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func (s *resolverRepository) Object(ctx context.Context, id string) (repository.Object, bool, error) {
	if s.finished {
		return nil, false, errors.New("repository session is finished")
	}
	if len(id) == 64 {
		return nil, false, errors.New("repository object access does not support SHA-256 object IDs")
	}
	if !validSHA(id) {
		return nil, false, errors.New("invalid Git object ID")
	}
	object, found, err := s.object(ctx, strings.ToLower(id), make(map[string]bool), 0)
	if err == nil {
		s.log = append(s.log, "object "+strings.ToLower(id))
	}
	return object, found, err
}

func (s *resolverRepository) object(ctx context.Context, id string, seen map[string]bool, depth int) (repository.Object, bool, error) {
	if depth > 16 || seen[id] {
		return nil, false, errors.New("git tag chain exceeds its limit or contains a cycle")
	}
	seen[id] = true
	for _, kind := range []string{"commits", "tags", "trees", "blobs"} {
		data, ok, err := s.get(ctx, "/git/"+kind+"/"+id)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		var body struct {
			SHA    string `json:"sha"`
			Object struct {
				SHA  string `json:"sha"`
				Type string `json:"type"`
			} `json:"object"`
		}
		if json.Unmarshal(data, &body) != nil || !validSHA(body.SHA) || !strings.EqualFold(body.SHA, id) {
			return nil, false, errors.New("invalid GitHub object response")
		}
		switch kind {
		case "blobs":
			return nil, false, errors.New("git blob objects cannot be resolved as commits or trees")
		case "commits":
			return repository.Commit{}, true, nil
		case "trees":
			return repository.Tree{}, true, nil
		case "tags":
			if !validSHA(body.Object.SHA) || (body.Object.Type != "commit" && body.Object.Type != "tree" && body.Object.Type != "tag") {
				return nil, false, errors.New("invalid annotated Git tag target")
			}
			target := strings.ToLower(body.Object.SHA)
			peeled, found, err := s.object(ctx, target, seen, depth+1)
			if err != nil {
				return nil, false, err
			}
			if !found {
				return nil, false, errors.New("annotated Git tag target is inaccessible")
			}
			switch v := peeled.(type) {
			case repository.Commit:
				if body.Object.Type != "commit" {
					break
				}
				return repository.PeeledTag{Commit: target}, true, nil
			case repository.Tree:
				if body.Object.Type != "tree" {
					break
				}
				return repository.PeeledTreeTag{Tree: target}, true, nil
			case repository.PeeledTag:
				if body.Object.Type != "tag" {
					break
				}
				return v, true, nil
			case repository.PeeledTreeTag:
				if body.Object.Type != "tag" {
					break
				}
				return v, true, nil
			}
			return nil, false, errors.New("annotated Git tag target type does not match")
		}
	}
	if err := s.guard(ctx); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

func (s *resolverRepository) Finish(ctx context.Context) (json.RawMessage, error) {
	if s.finished {
		return nil, errors.New("repository session is finished")
	}
	s.finished = true
	names := make([]string, 0, len(s.refs))
	for name := range s.refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		observed, err := s.readReference(ctx, name)
		if err != nil {
			return nil, err
		}
		if observed != s.refs[name] {
			return nil, errors.New("git reference changed during resolution")
		}
	}
	repo, err := s.readRepository(ctx)
	if err != nil {
		return nil, err
	}
	if id, _ := s.identity.ID.Value(); repo.ID != id || !strings.EqualFold(repo.Name, s.identity.Name) || repo.DefaultBranch != s.defaultBranch {
		return nil, errors.New("GitHub repository identity or default branch changed during resolution")
	}
	log, err := json.Marshal(struct {
		Host         string   `json:"host"`
		Observations []string `json:"observations"`
	}{Host: "github.com", Observations: s.log})
	return log, err
}
