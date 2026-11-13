package typecheck

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestExprSemanticsCheckRealWorld(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join("testdata", "expressions.txt"))
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	s := bufio.NewScanner(f)
	for s.Scan() {
		expr := s.Text()
		l := exprsyntax.NewLexer(expr + "}}")
		p := exprsyntax.NewParser()
		root, err := p.Parse(l)
		if err != nil {
			t.Errorf("%q caused parse error: %v", expr, err)
			continue
		}
		c := NewChecker(contextspec.DefaultExpressionScope(), nil)
		c.Check(expreval.EvaluateSubexpressions(root))
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkExprRealWorld(b *testing.B) {
	f, err := os.Open(filepath.Join("testdata", "expressions.txt"))
	if err != nil {
		b.Fatal(err)
	}

	exprs := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		exprs = append(exprs, s.Text()+"}}")
	}
	_ = f.Close()
	if err := s.Err(); err != nil {
		b.Fatal(err)
	}

	b.Run("Lex", func(b *testing.B) {
		for range b.N {
			for _, expr := range exprs {
				l := exprsyntax.NewLexer(expr)
				for {
					t := l.Next()
					if err := l.Err(); err != nil {
						b.Fatal(err)
					}
					if t.Kind == exprsyntax.TokenKindEnd {
						break
					}
				}
			}
		}
	})

	b.Run("LexParse", func(b *testing.B) {
		for range b.N {
			for _, expr := range exprs {
				if _, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(expr)); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("LexParseSema", func(b *testing.B) {
		for range b.N {
			for _, expr := range exprs {
				root, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(expr + "}}"))
				if err != nil {
					b.Fatalf("%q caused parse error: %v", expr, err)
				}
				NewChecker(contextspec.DefaultExpressionScope(), nil).Check(expreval.EvaluateSubexpressions(root))
			}
		}
	})

	trees := []exprsyntax.Node{}
	for range b.N {
		for _, expr := range exprs {
			t, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(expr))
			if err != nil {
				b.Fatal(err)
			}
			trees = append(trees, t)
		}
	}

	b.Run("Typecheck", func(b *testing.B) {
		for range b.N {
			for _, t := range trees {
				NewChecker(contextspec.DefaultExpressionScope(), nil).Check(expreval.EvaluateSubexpressions(t))
			}
		}
	})
}
