package expression

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
)

func TestExprSemanticsCheckRealWorld(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "testdata", "bench", "expressions.txt"))
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	s := bufio.NewScanner(f)
	for s.Scan() {
		expr := s.Text()
		l := NewLexer(expr + "}}")
		p := NewParser()
		root, err := p.Parse(l)
		if err != nil {
			t.Errorf("%q caused parse error: %v", expr, err)
			continue
		}
		c := NewChecker(true, nil)
		c.Check(root)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkExprRealWorld(b *testing.B) {
	f, err := os.Open(filepath.Join("..", "..", "testdata", "bench", "expressions.txt"))
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
				l := NewLexer(expr)
				for {
					t := l.Next()
					if l.lexErr != nil {
						b.Fatal(l.lexErr)
					}
					if t.Kind == TokenKindEnd {
						break
					}
				}
			}
		}
	})

	b.Run("LexParse", func(b *testing.B) {
		for range b.N {
			for _, expr := range exprs {
				if _, err := NewParser().Parse(NewLexer(expr)); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("LexParseSema", func(b *testing.B) {
		for range b.N {
			for _, expr := range exprs {
				root, err := NewParser().Parse(NewLexer(expr + "}}"))
				if err != nil {
					b.Fatalf("%q caused parse error: %v", expr, err)
				}
				NewChecker(true, nil).Check(root)
			}
		}
	})

	trees := []Node{}
	for range b.N {
		for _, expr := range exprs {
			t, err := NewParser().Parse(NewLexer(expr))
			if err != nil {
				b.Fatal(err)
			}
			trees = append(trees, t)
		}
	}

	b.Run("Sema-untrust", func(b *testing.B) {
		for range b.N {
			for _, t := range trees {
				NewChecker(true, nil).Check(t)
			}
		}
	})

	b.Run("Sema-trust", func(b *testing.B) {
		for range b.N {
			for _, t := range trees {
				NewChecker(false, nil).Check(t)
			}
		}
	})
}
