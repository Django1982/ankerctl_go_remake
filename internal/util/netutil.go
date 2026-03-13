package util

import "net"

// IsValidPrinterIP reports whether ip is a usable unicast LAN address for a
// printer. It rejects nil, unspecified (0.0.0.0), loopback (127.x.x.x),
// broadcast (255.255.255.255), and link-local (169.254.x.x) addresses.
func IsValidPrinterIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false // IPv6 not supported by PPPP
	}
	if ip4.IsUnspecified() || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
		return false
	}
	// Broadcast: 255.255.255.255
	if ip4.Equal(net.IPv4bcast) {
		return false
	}
	return true
}

// IsValidPrinterIPString is a convenience wrapper that parses a string IP
// and validates it with IsValidPrinterIP.
func IsValidPrinterIPString(ipStr string) bool {
	if ipStr == "" {
		return false
	}
	ip := net.ParseIP(ipStr)
	return IsValidPrinterIP(ip)
}
