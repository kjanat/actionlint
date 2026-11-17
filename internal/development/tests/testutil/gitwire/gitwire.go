// Package gitwire writes Git smart HTTP protocol responses for test fixtures.
package gitwire

import (
	"fmt"
	"strings"
)

const ContentType = "application/x-git-upload-pack-advertisement"

type Ref struct {
	Name, Object, Peeled string
}

func RefAdvertisement(head, symref string, refs ...Ref) []byte {
	var out strings.Builder
	packet(&out, "# service=git-upload-pack\n")
	out.WriteString("0000")
	capabilities := "multi_ack side-band-64k object-format=sha1 agent=git/github-fixture"
	if symref != "" {
		capabilities = "symref=HEAD:" + symref + " " + capabilities
	}
	if head == "" {
		packet(&out, strings.Repeat("0", 40)+" capabilities^{}\x00"+capabilities+"\n")
	} else {
		packet(&out, head+" HEAD\x00"+capabilities+"\n")
	}
	for _, ref := range refs {
		packet(&out, ref.Object+" "+ref.Name+"\n")
		if ref.Peeled != "" {
			packet(&out, ref.Peeled+" "+ref.Name+"^{}\n")
		}
	}
	out.WriteString("0000")
	return []byte(out.String())
}

func packet(out *strings.Builder, payload string) {
	fmt.Fprintf(out, "%04x%s", len(payload)+4, payload)
}
