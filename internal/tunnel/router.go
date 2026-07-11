package tunnel

import (
	"net"
	"strings"
)

// Router decides which destinations are sent through the SSH tunnel.
// A matched host goes via SSH; everything else connects directly (split tunnel).
type Router struct {
	matchAll bool
	entries  []routeEntry
}

type routeEntry struct {
	domain string
	ip     net.IP
	cidr   *net.IPNet
}

// NewRouter builds a Router from a list of domains, IPs, or CIDR ranges.
// The special entries "*", "0.0.0.0/0" and "::/0" match everything.
func NewRouter(list []string) *Router {
	r := &Router{}
	for _, raw := range list {
		item := strings.ToLower(strings.TrimSpace(raw))
		if item == "" {
			continue
		}
		if item == "*" || item == "0.0.0.0/0" || item == "::/0" {
			r.matchAll = true
			continue
		}
		if _, ipnet, err := net.ParseCIDR(item); err == nil {
			r.entries = append(r.entries, routeEntry{cidr: ipnet})
			continue
		}
		if ip := net.ParseIP(item); ip != nil {
			r.entries = append(r.entries, routeEntry{ip: ip})
			continue
		}
		r.entries = append(r.entries, routeEntry{domain: item})
	}
	return r
}

// Match reports whether host should be routed through the SSH tunnel.
func (r *Router) Match(host string) bool {
	if r == nil {
		return false
	}
	if r.matchAll {
		return true
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, e := range r.entries {
		if e.domain != "" {
			if host == e.domain || strings.HasSuffix(host, "."+e.domain) {
				return true
			}
		} else if e.ip != nil {
			if ip := net.ParseIP(host); ip != nil && ip.Equal(e.ip) {
				return true
			}
		} else if e.cidr != nil {
			if ip := net.ParseIP(host); ip != nil && e.cidr.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// dialForTarget connects to target directly (bypassing the tunnel) when it
// matches the blacklist; otherwise it goes through the SSH tunnel.
func dialForTarget(target string, engine *Engine, router *Router) (net.Conn, error) {
	if router != nil && router.Match(target) {
		return net.Dial("tcp", target)
	}
	client := engine.currentClient()
	if client == nil {
		return nil, errTunnelDown
	}
	return client.Dial("tcp", target)
}
