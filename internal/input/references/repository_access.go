package references

import (
	"context"
	"encoding/json"

	repofacts "actionlint.kjanat.dev/v2/internal/model/repository"
)

type RepositoryIdentity struct {
	Host       string
	ID         repofacts.ID
	Name       string
	ObservedAt string
}
type RepositoryAccess interface {
	Open(context.Context, Request) (RepositorySession, error)
}
type RepositorySession interface {
	Repository() RepositoryIdentity
	Reference(context.Context, string) (string, bool, error)
	Object(context.Context, string) (RepositoryObject, bool, error)
	Finish(context.Context) (json.RawMessage, error)
}

// Object identifies a commit, tree or annotated tag returned by the repository.
type (
	RepositoryObject        interface{ repositoryObject() }
	RepositoryCommit        struct{}
	RepositoryTree          struct{}
	RepositoryPeeledTag     struct{ Commit string }
	RepositoryPeeledTreeTag struct{ Tree string }
)

func (RepositoryCommit) repositoryObject()        {}
func (RepositoryTree) repositoryObject()          {}
func (RepositoryPeeledTag) repositoryObject()     {}
func (RepositoryPeeledTreeTag) repositoryObject() {}

type accessKey struct{}

func WithRepositoryAccess(ctx context.Context, access RepositoryAccess) context.Context {
	return context.WithValue(ctx, accessKey{}, access)
}

func RepositoryAccessFromContext(ctx context.Context) RepositoryAccess {
	access, _ := ctx.Value(accessKey{}).(RepositoryAccess)
	return access
}
