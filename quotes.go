package actionlint

import (
	"sort"
	"strconv"
	"strings"
)

type quotesBuilder struct {
	inner strings.Builder
	buf   []byte
	comma bool
}

func (b *quotesBuilder) append(s string) {
	if b.comma {
		b.inner.WriteString(", ")
	} else {
		b.comma = true
	}
	b.buf = strconv.AppendQuote(b.buf[:0], s)
	b.inner.Write(b.buf)
}

func (b *quotesBuilder) appendRune(r rune) {
	if b.comma {
		b.inner.WriteString(", ")
	} else {
		b.comma = true
	}
	b.buf = strconv.AppendQuoteRune(b.buf[:0], r)
	b.inner.Write(b.buf)
}

func (b *quotesBuilder) build() string {
	return b.inner.String()
}

func quotes(ss []string) string {
	l := len(ss)
	if l == 0 {
		return ""
	}
	n, longest := 0, 0
	for _, s := range ss {
		m := len(s) + 2 // 2 for delims
		n += m
		if m > longest {
			longest = m
		}
	}
	n += (l - 1) * 2 // comma
	b := quotesBuilder{}
	b.buf = make([]byte, 0, longest)
	b.inner.Grow(n)
	for _, s := range ss {
		b.append(s)
	}
	return b.build()
}

func sortedQuotes(ss []string) string {
	sort.Strings(ss)
	return quotes(ss)
}

func quotesAll(sss ...[]string) string {
	n, longest := 0, 0
	for _, ss := range sss {
		for _, s := range ss {
			m := len(s) + 2 // 2 for delims
			n += m
			if m > longest {
				longest = m
			}
		}
		n += (len(ss) - 1) * 2 // comma
	}
	b := quotesBuilder{}
	b.buf = make([]byte, 0, longest)
	n += (len(sss) - 1) * 2 // comma
	if n > 0 {
		b.inner.Grow(n)
	}
	for _, ss := range sss {
		for _, s := range ss {
			b.append(s)
		}
	}
	return b.build()
}
