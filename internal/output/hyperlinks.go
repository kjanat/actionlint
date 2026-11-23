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

// An OSC 8 hyperlink ends with terminalLinkOpen and terminalLinkClose around an empty target.
// https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feff
const (
	terminalLinkOpen  = "\x1b]8;;"
	terminalLinkClose = "\x1b\\"
)

func writeTerminalLinkStart(out *strings.Builder, target string) {
	out.WriteString(terminalLinkOpen)
	out.WriteString(target)
	out.WriteString(terminalLinkClose)
}

func writeTerminalLinkEnd(out *strings.Builder) {
	out.WriteString(terminalLinkOpen + terminalLinkClose)
}

// TerminalLink leaves label unlinked when target is empty.
func TerminalLink(label, target string) string {
	if target == "" {
		return label
	}
	var link strings.Builder
	writeTerminalLinkStart(&link, target)
	link.WriteString(label)
	writeTerminalLinkEnd(&link)
	return link.String()
}
