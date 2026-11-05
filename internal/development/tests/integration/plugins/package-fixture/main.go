// Command package-fixture adds root metadata to a component used by integration tests.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: package-fixture component.wasm metadata.json output.wasm")
	}
	wasm, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	metadata, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	packaged, err := packageMetadata(wasm, metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(args[2], packaged, 0o644)
}

func packageMetadata(wasm, metadata []byte) ([]byte, error) {
	kind, existing, err := artifacts.Sniff(wasm)
	if err != nil {
		return nil, err
	}
	if kind != artifacts.Component || existing != nil || !json.Valid(metadata) || len(metadata) > 1<<20 {
		return nil, errors.New("fixture needs a component without root metadata and valid metadata within 1 MiB")
	}
	name := "actionlint.plugin"
	payload := binary.AppendUvarint(nil, uint64(len(name)))
	payload = append(payload, name...)
	payload = append(payload, metadata...)
	packaged := append([]byte(nil), wasm...)
	packaged = append(packaged, 0)
	packaged = binary.AppendUvarint(packaged, uint64(len(payload)))
	return append(packaged, payload...), nil
}
