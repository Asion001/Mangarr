// Package netguard makes HTTP clients for URLs that come from user data
// (e.g. cover links in an uploaded backup) that can't reach the local
// network, loopback or cloud metadata addresses.
package netguard

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrBlocked is returned for connections to non-public addresses.
var ErrBlocked = errors.New("address not allowed")

// Public reports whether ip is a public unicast address.
func Public(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast())
}

// Client returns an HTTP client that only connects to public addresses
// (checked at connect time, so DNS answers can't point it elsewhere).
func Client(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip == nil || !Public(ip) {
			return ErrBlocked
		}
		return nil
	}}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = d.DialContext
	tr.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: tr}
}
