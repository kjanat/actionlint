package references

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ValidateResult checks the response version, repository identity, object ID and fetch log.
func ValidateResult(request Request, result Result) error {
	if result.Version != 1 {
		return fmt.Errorf("unsupported resolver response version %d", result.Version)
	}
	if result.Error != "" {
		return fmt.Errorf("resolution failed: %s", result.Error)
	}
	if !strings.EqualFold(result.Repository, request.Repository) || result.RepositoryID.Conflicts(request.ExpectedRepositoryID) || (result.Commit == "") == (result.Tree == "") || result.ObservedAt == "" || len(result.FetchLog) == 0 {
		return errors.New("resolver returned incomplete or inconsistent provenance")
	}
	object := result.Commit
	if object == "" {
		object = result.Tree
	}
	if len(object) != 40 && len(object) != 64 {
		return errors.New("resolver returned invalid object ID")
	}
	for _, c := range object {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return errors.New("resolver returned invalid object ID")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, result.ObservedAt); err != nil {
		return errors.New("resolver returned invalid observation time")
	}
	if bytes.Equal(bytes.TrimSpace(result.FetchLog), []byte("null")) {
		return errors.New("resolver returned no fetch log")
	}
	return nil
}
