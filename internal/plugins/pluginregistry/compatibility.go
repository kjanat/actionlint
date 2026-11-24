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

type recordedRejectionError string

func (r recordedRejectionError) Error() string { return string(r) }

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
		if _, ok := v.interfaces(); !ok {
			continue
		}
		pin, err := resolve(v)
		if err != nil {
			return nil, err
		}
		listed[i].Host = HostSupport{Status: SupportCompatible}
		_, err = c.checkRelease(ctx, pin, v, true)
		if incompatible, ok := errors.AsType[*IncompatibleError](err); ok {
			listed[i].Host = HostSupport{Status: SupportIncompatible, Reason: incompatible.Err.Error()}
		} else if err != nil {
			return nil, err
		}
	}
	return listed, nil
}

func (c *Client) checkRelease(ctx context.Context, pin Pin, v Version, download bool) (component.Roles, error) {
	if pin.Kind != artifacts.Component {
		return component.Roles{}, nil
	}
	reason, rejected, err := c.rejection(ctx, pin)
	if err != nil {
		return component.Roles{}, err
	}
	if rejected {
		return component.Roles{}, &IncompatibleError{Name: pin.Name, Version: pin.Version, Err: recordedRejectionError(reason)}
	}
	var listed component.Roles
	if interfaces, ok := v.interfaces(); ok {
		if listed, err = admit(ctx, pin, component.ListedInterfaces{Interfaces: interfaces, Metadata: []byte(pin.Metadata)}); err != nil {
			return component.Roles{}, err
		}
	}
	if !download {
		return listed, nil
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
	return checkArtifact(ctx, pin, artifact)
}

func checkArtifact(ctx context.Context, pin Pin, artifact *artifacts.Artifact) (component.Roles, error) {
	if pin.Kind != artifacts.Component {
		return component.Roles{}, nil
	}
	return admit(ctx, pin, component.ComponentBytes(artifact.Bytes))
}

func admit(ctx context.Context, pin Pin, evidence component.Evidence) (component.Roles, error) {
	roles, err := component.Admit(ctx, evidence)
	if rejected, ok := errors.AsType[*component.AdmissionError](err); ok {
		return component.Roles{}, &IncompatibleError{Name: pin.Name, Version: pin.Version, Err: rejected}
	}
	return roles, err
}
