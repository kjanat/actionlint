package output

import (
	"net/url"
	"regexp"
	"strings"
)

var terminalURLPattern = regexp.MustCompile("(?i)https?://[^\\s<>\"'`\\x{FFFD}]+")

type terminalURL struct {
	start, end int
	target     string
}

func terminalURLs(word string) []terminalURL {
	var links []terminalURL
	for _, match := range terminalURLPattern.FindAllStringIndex(word, -1) {
		start, end := match[0], match[1]
		for end > start {
			last := word[end-1]
			if strings.ContainsRune(".,;:!?", rune(last)) ||
				last == ')' && strings.Count(word[start:end], ")") > strings.Count(word[start:end], "(") ||
				last == ']' && strings.Count(word[start:end], "]") > strings.Count(word[start:end], "[") ||
				last == '}' && strings.Count(word[start:end], "}") > strings.Count(word[start:end], "{") {
				end--
				continue
			}
			break
		}
		target := word[start:end]
		parsed, err := url.Parse(target)
		if err == nil && parsed.Hostname() != "" {
			links = append(links, terminalURL{start: start, end: end, target: target})
		}
	}
	return links
}

func writeTerminalLinkStart(out *strings.Builder, target string) {
	out.WriteString("\x1b]8;;")
	out.WriteString(target)
	out.WriteString("\x1b\\")
}

func writeTerminalLinkEnd(out *strings.Builder) { out.WriteString("\x1b]8;;\x1b\\") }
