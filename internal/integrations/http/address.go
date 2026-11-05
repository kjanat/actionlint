package acquisitionhttp

import (
	"context"
	"errors"
	"net/netip"
)

func (t *transport) publicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	addresses := []netip.Addr{}
	if ip, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, ip)
	} else {
		if t.lookup == nil {
			return nil, ErrDNSFailed
		}
		resolved, err := t.lookup(ctx, "ip", host)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return nil, context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, context.DeadlineExceeded
			}
			return nil, ErrDNSFailed
		}
		addresses = append(addresses, resolved...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, ErrPrivateDestination
	}
	for i, ip := range addresses {
		ip = ip.Unmap()
		if !publicAddress(ip) {
			return nil, ErrPrivateDestination
		}
		addresses[i] = ip
	}
	return addresses, nil
}

// Special-purpose/transition ranges are not ordinary public targets. This
// conservative policy excludes protocol-assignment ranges, including globally
// reachable exceptions, and IPv6 outside global 2000::/3.
// Sources: IANA IPv4 and IPv6 Special-Purpose Address Space registries.
var specialRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var ipv6Global = netip.MustParsePrefix("2000::/3")

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !ipv6Global.Contains(ip) {
		return false
	}
	for _, prefix := range specialRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
