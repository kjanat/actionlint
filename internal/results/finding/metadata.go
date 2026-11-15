package finding

type MetadataKey struct {
	Path, Message string
	Line, Column  int
}

type MetadataIdentity struct {
	Key         MetadataKey
	Visit       uint64
	Declaration bool
}
