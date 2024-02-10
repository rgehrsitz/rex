package validation

import (
	"net"
	"regexp"
)

// IsValidStoreKey checks if a given key is valid for the store.
func IsValidStoreKey(key string) bool {
	// Define the allowed pattern for keys
	var validKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	return validKeyPattern.MatchString(key)
}

// IsValidAddress checks if a given network address is valid.
func IsValidAddress(address string) bool {
	if ip := net.ParseIP(address); ip != nil {
		return true
	}
	// Check for valid host:port pair if needed
	host, _, err := net.SplitHostPort(address)
	return err == nil && net.ParseIP(host) != nil
}
