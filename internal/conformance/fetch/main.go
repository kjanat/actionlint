package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"actionlint.kjanat.dev/internal/conformance"
)

func main() {
	dir := flag.String("dir", ".cache/conformance", "Directory for verified upstream archives")
	flag.Parse()
	if err := fetch(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fetch(dir string) error {
	sources, err := conformance.Sources()
	if err != nil {
		return err
	}
	for _, s := range sources {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		err := s.Fetch(ctx, dir)
		cancel()
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		fmt.Printf("Verified %s@%s\n", s.Repository, s.Revision)
	}
	return nil
}
