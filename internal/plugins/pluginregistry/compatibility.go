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

type ListedVersion struct {
	Version
	Host HostSupport `json:"host"`
}

func (c *Client) ListVersions(ctx context.Context, name string) ([]ListedVersion, error) {
	var versions []Version
	var resolve func(Version) (Pin, error)
	if c.base.Scheme == "file" {
		p, m, err := c.candidateManifest(ctx, name)
		if err != nil {
			return nil, err
		}
		versions = m.Versions
		resolve = func(v Version) (Pin, error) { return c.candidatePin(ctx, p, v, v.Version) }
	} else {
		p, m, err := c.manifest(ctx, name)
		if err != nil {
			return nil, err
		}
		versions = m.Versions
		resolve = func(v Version) (Pin, error) { return c.resolveVersion(ctx, p, m, v, v.Version) }
	}
	listed := make([]ListedVersion, len(versions))
	for i, v := range versions {
		listed[i] = ListedVersion{Version: v, Host: HostSupport{Status: SupportUnknown}}
		interfaces, ok := v.interfaces()
		if !ok {
			continue
		}
		pin, err := resolve(v)
		if err != nil {
			return nil, err
		}
		listed[i].Host = HostSupport{Status: SupportCompatible}
		if _, err := component.Admit(component.ListedInterfaces{Interfaces: interfaces, Metadata: []byte(pin.Metadata)}); err != nil {
			listed[i].Host = HostSupport{Status: SupportIncompatible, Reason: err.Error()}
		}
	}
	return listed, nil
}

func (c *Client) checkRelease(ctx context.Context, pin Pin, v Version) (component.Roles, error) {
	if pin.Kind != artifacts.Component {
		return component.Roles{}, nil
	}
	if interfaces, ok := v.interfaces(); ok {
		return admit(pin, component.ListedInterfaces{Interfaces: interfaces, Metadata: []byte(pin.Metadata)})
	}
	options := pluginacquisition.ArtifactOptions(c.StatusOptions.Acquisition)
	options.Network = c.StatusOptions.Network
	if pin.Origin == "candidate" {
		if err := pin.importCandidate(ctx, options); err != nil {
			return component.Roles{}, fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
	}
	artifact, err := artifacts.Resolve(ctx, pin.Source(), options)
	if err != nil {
		return component.Roles{}, fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
	}
	if err := pin.Verify(artifact.Info); err != nil {
		return component.Roles{}, fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
	}
	return checkArtifact(pin, artifact)
}

func checkArtifact(pin Pin, artifact *artifacts.Artifact) (component.Roles, error) {
	if pin.Kind != artifacts.Component {
		return component.Roles{}, nil
	}
	return admit(pin, component.ComponentBytes(artifact.Bytes))
}

func admit(pin Pin, evidence component.Evidence) (component.Roles, error) {
	roles, err := component.Admit(evidence)
	if err != nil {
		return component.Roles{}, &IncompatibleError{Name: pin.Name, Version: pin.Version, Err: err}
	}
	return roles, nil
}
