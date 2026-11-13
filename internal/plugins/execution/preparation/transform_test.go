package preparation

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestSourceTransformRejectsMalformedPlans(t *testing.T) {
	t.Parallel()
	for _, source := range []string{`null`, `[]`, `{source_prefix:"unterminated"}`, `{unknown:true}`, `{startup:[]}`, `{variables:{mode:42}}`, `{}, {}`} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			transform, err := NewSourceTransform(&manifests.Transform{Language: manifests.LanguageJQ, Inline: source}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transform.Apply(t.Context(), ScriptContext{}); err == nil {
				t.Fatal("accepted invalid preparation plan")
			}
		})
	}
}

func TestSourceTransformHonorsCancellation(t *testing.T) {
	t.Parallel()
	transform, err := NewSourceTransform(&manifests.Transform{Language: manifests.LanguageJQ, Inline: `def loop: loop; loop`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := transform.Apply(ctx, ScriptContext{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
