package preparation

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

func TestSourcePlannerRejectsMalformedPlans(t *testing.T) {
	t.Parallel()
	for _, source := range []string{`null`, `[]`, `{source_prefix:"unterminated"}`, `{unknown:true}`, `{startup:[]}`, `{variables:{mode:42}}`, `{}, {}`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			transform, err := NewSourcePlanner(&toolmanifest.Transform{Language: toolmanifest.LanguageJQ, Inline: source}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transform.Plan(t.Context(), SourcePlanInput{}); err == nil {
				t.Fatal("accepted invalid preparation plan")
			}
		})
	}
}

func TestSourcePlannerHonorsCancellation(t *testing.T) {
	t.Parallel()
	transform, err := NewSourcePlanner(&toolmanifest.Transform{Language: toolmanifest.LanguageJQ, Inline: `def loop: loop; loop`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := transform.Plan(ctx, SourcePlanInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
