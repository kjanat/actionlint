package actionlint

func (cfg *Config) cachePolicyEnabled(name string) bool {
	var enabled *bool
	if cfg != nil {
		switch name {
		case "cache-write-untrusted":
			enabled = cfg.Policy.CacheWriteUntrusted
		case "cache-call-unrestricted":
			enabled = cfg.Policy.CacheCallUnrestricted
		case "cache-operation":
			enabled = cfg.Policy.CacheOperation
		}
	}
	return enabled == nil || *enabled
}

func isCachePolicy(name string) bool {
	switch name {
	case "cache-write-untrusted", "cache-call-unrestricted", "cache-operation":
		return true
	default:
		return false
	}
}
