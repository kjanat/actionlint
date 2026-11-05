package config

func (cfg *Config) CachePolicyEnabled(name string) bool {
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
