package repository_test

import (
	"actionlint.kjanat.dev/v2/knowledge"
	"actionlint.kjanat.dev/v2/repository"
	"testing"
)

func TestUnknownDiffersFromKnownEmptyFacts(t *testing.T) {
	var unknown repository.Snapshot
	names, err := repository.NewNames()
	if err != nil {
		t.Fatal(err)
	}
	variables, err := repository.NewVariables()
	if err != nil {
		t.Fatal(err)
	}
	environments, err := repository.NewEnvironments()
	if err != nil {
		t.Fatal(err)
	}
	if unknown.RunnerLabels.Available() || unknown.Variables.Available() || unknown.Environments.Available() || unknown.DefaultWorkflowPermissions.Available() {
		t.Fatal("zero snapshot fabricated repository facts")
	}
	if !names.Available() || names.Values() == nil || !variables.Available() || !environments.Available() {
		t.Fatal("known empty facts became unavailable")
	}
}

func TestSnapshotsOwnNameAndValueCollections(t *testing.T) {
	source := []string{"TOKEN"}
	names, err := repository.NewNames(source...)
	if err != nil {
		t.Fatal(err)
	}
	source[0] = "CHANGED"
	returned := names.Values()
	returned[0] = "CHANGED"
	if names.Values()[0] != "TOKEN" {
		t.Fatal("name collection retained shared mutable storage")
	}
	variables := []repository.Variable{{Name: "BUILD", Value: knowledge.Available("original")}}
	values, err := repository.NewVariables(variables...)
	if err != nil {
		t.Fatal(err)
	}
	variables[0] = repository.Variable{Name: "CHANGED"}
	entries := values.Entries()
	entries[0] = repository.Variable{Name: "CHANGED"}
	value, known := values.Entries()[0].Value.Value()
	if values.Names()[0] != "BUILD" || !known || value != "original" {
		t.Fatal("variable collection retained shared mutable storage")
	}
	environments := []repository.Environment{{Name: "production", Secrets: names}}
	available, err := repository.NewEnvironments(environments...)
	if err != nil {
		t.Fatal(err)
	}
	environments[0].Name = "CHANGED"
	output := available.Entries()
	output[0].Name = "CHANGED"
	if available.Entries()[0].Name != "production" {
		t.Fatal("environment collection retained shared mutable storage")
	}
}
