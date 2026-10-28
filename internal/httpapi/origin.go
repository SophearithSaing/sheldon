package httpapi

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
)

// ValidateOrigin accepts a canonical numeric-loopback HTTP origin with an explicit port.
// Paths, credentials, query strings, fragments, DNS names and wildcard binds are rejected.
func ValidateOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return errors.New("expected a canonical numeric-loopback HTTP origin with an explicit port")
	}
	ip, err := netip.ParseAddr(u.Hostname())
	port, portErr := strconv.Atoi(u.Port())
	if err != nil || !ip.IsLoopback() || ip.Is4In6() || ip.Zone() != "" || portErr != nil || port < 1 || port > 65535 || origin != "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(port)) {
		return errors.New("expected a canonical numeric-loopback HTTP origin with an explicit port")
	}
	return nil
}
