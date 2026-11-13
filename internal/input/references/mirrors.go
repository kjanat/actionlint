package references

// MirrorAccess controls whether metadata may be requested from a third-party mirror.
type MirrorAccess uint8

const (
	PublicMirrorsOnly MirrorAccess = iota
	AllowUnknownMirrors
	NoMirrors
)
