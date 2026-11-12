// Package repository supplies Git references and objects to resolvers.
package repository

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type Identity struct {
	Host       string
	ID         repositoryid.ID
	Name       string
	ObservedAt string
}
type Access interface {
	Open(context.Context, references.Request) (Session, error)
}
type Session interface {
	Repository() Identity
	Reference(context.Context, string) (string, bool, error)
	Object(context.Context, string) (Object, bool, error)
	Finish(context.Context) (json.RawMessage, error)
}

// Object identifies a commit, tree or annotated tag returned by the repository.
type (
	Object        interface{ repositoryObject() }
	Commit        struct{}
	Tree          struct{}
	PeeledTag     struct{ Commit string }
	PeeledTreeTag struct{ Tree string }
)

func (Commit) repositoryObject()        {}
func (Tree) repositoryObject()          {}
func (PeeledTag) repositoryObject()     {}
func (PeeledTreeTag) repositoryObject() {}

type accessKey struct{}

func WithAccess(ctx context.Context, access Access) context.Context {
	return context.WithValue(ctx, accessKey{}, access)
}

func AccessFromContext(ctx context.Context) Access {
	access, _ := ctx.Value(accessKey{}).(Access)
	return access
}
