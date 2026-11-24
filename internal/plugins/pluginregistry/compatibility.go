package pluginregistry

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

var ErrNoCompatibleRelease = errors.New("no published release loads in this actionlint")

type IncompatibleError struct {
	Name, Version string
	Err           error
}

func (e *IncompatibleError) Error() string {
	return fmt.Sprintf("plugin %s@%s does not load in this actionlint: %v", e.Name, e.Version, e.Err)
}

func (e *IncompatibleError) Unwrap() error { return e.Err }

type SupportStatus string

const (
	SupportCompatible   SupportStatus = "compatible"
	SupportIncompatible SupportStatus = "incompatible"
	SupportUnknown      SupportStatus = "unknown"
)

type HostSupport struct {
	Status SupportStatus `json:"status"`
	Reason string        `json:"reason,omitempty"`
}

func (v Version) HostSupport() HostSupport {
	interfaces, ok := v.interfaces()
	if !ok {
		return HostSupport{Status: SupportUnknown}
	}
	if _, err := component.CheckInterfaces(interfaces); err != nil {
		return HostSupport{Status: SupportIncompatible, Reason: err.Error()}
	}
	return HostSupport{Status: SupportCompatible}
}

func (c *Client) checkRelease(ctx context.Context, pin Pin, v Version) error {
	if pin.Kind != artifacts.Component {
		return nil
	}
	if interfaces, ok := v.interfaces(); ok {
		if _, err := component.CheckInterfaces(interfaces); err != nil {
			return &IncompatibleError{Name: pin.Name, Version: pin.Version, Err: err}
		}
		return nil
	}
	options := pluginacquisition.ArtifactOptions(c.StatusOptions.Acquisition)
	options.Network = c.StatusOptions.Network
	artifact, err := artifacts.Resolve(ctx, pin.Source(), options)
	if err != nil {
		return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
	}
	if err := pin.Verify(artifact.Info); err != nil {
		return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
	}
	return checkArtifact(pin, artifact)
}

func checkArtifact(pin Pin, artifact *artifacts.Artifact) error {
	if pin.Kind != artifacts.Component {
		return nil
	}
	if _, err := component.CheckComponent(artifact.Bytes); err != nil {
		return &IncompatibleError{Name: pin.Name, Version: pin.Version, Err: err}
	}
	return nil
}
