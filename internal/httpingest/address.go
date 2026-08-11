package httpingest

import (
	"net"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ValidateLoopbackAddress rejects wildcard and non-loopback listener addresses.
func ValidateLoopbackAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address is required", nil)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address is invalid", err)
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "localhost":
		return nil
	case "":
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address must bind loopback only", nil)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return memory.PermanentError(memory.CodeInvalidDerived, "listener address must bind loopback only", nil)
	}
	return nil
}
