package component

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"github.com/kjanat/wacogo"
)

func TestToolNegotiationPreservesExplicitGrants(t *testing.T) {
	value := wacogo.ValResultOk(wacogo.NewValRecord(
		field("options", wacogo.ValString(`{"engine-option":true}`)),
		field("resources", wacogo.NewValListOf(wacogo.NewValVariant(0, nil), wacogo.NewValVariant(1, wacogo.ValString("/other/source")))),
	))
	got, err := liftToolNegotiation(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Options) != `{"engine-option":true}` || len(got.Resources) != 2 || got.Resources[0].Kind != analyzer.ToolWorkspace || got.Resources[1].Path != "/other/source" {
		t.Fatalf("unexpected negotiation: %#v", got)
	}
}

func TestUnsupportedToolConfigurationIsDistinctFromFailure(t *testing.T) {
	got, err := liftToolNegotiation(wacogo.ValResultErr(wacogo.ValString("unsupported option")))
	var unsupported *analyzer.UnsupportedToolConfiguration
	if got != nil || !errors.As(err, &unsupported) || unsupported.Reason != "unsupported option" {
		t.Fatalf("got %v, %v", got, err)
	}
	_, err = liftToolNegotiation(wacogo.ValString("malformed"))
	if err == nil || errors.As(err, &unsupported) {
		t.Fatalf("malformed result must fail: %v", err)
	}
}

func TestToolNegotiationRejectsMalformedOptionsAndResources(t *testing.T) {
	for _, value := range []wacogo.Val{
		wacogo.ValResultOk(wacogo.NewValRecord(field("options", wacogo.ValString(`[]`)), field("resources", wacogo.NewValListOf[wacogo.Val]()))),
		wacogo.ValResultOk(wacogo.NewValRecord(field("options", wacogo.ValString(`{}`)), field("resources", wacogo.NewValListOf(wacogo.NewValVariant(9, nil))))),
	} {
		if _, err := liftToolNegotiation(value); err == nil {
			t.Fatal("accepted invalid negotiation")
		}
	}
}

func TestSourceFilesStayCallScoped(t *testing.T) {
	roots := []analyzer.SourceRoot{{Host: "/project", Guest: "/workspace"}}
	ctx := context.WithValue(context.Background(), sourceRootsKey{}, roots)
	got, err := (sourceFiles{}).Roots(ctx)
	if err != nil || len(got) != 1 || got[0].Host != "/project" {
		t.Fatalf("got %v, %v", got, err)
	}
	got[0].Host = "changed"
	if roots[0].Host != "/project" {
		t.Fatal("import exposed caller slice")
	}
	empty, err := (sourceFiles{}).Roots(context.Background())
	if err != nil || len(empty) != 0 {
		t.Fatalf("roots leaked: %v, %v", empty, err)
	}
}
