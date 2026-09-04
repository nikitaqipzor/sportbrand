// Package ids generates the random identifiers used across the service.
package ids

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

// NewUUID returns a random RFC 4122 version 4 UUID.
func NewUUID() string {
	var b [16]byte
	// crypto/rand.Read never fails on supported platforms since Go 1.24.
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

// NewOpaqueToken returns n bytes of entropy encoded as an URL-safe string.
func NewOpaqueToken(n int) string {
	if n < 16 {
		n = 16
	}
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// IsUUID reports whether s looks like a canonical UUID.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHex(byte(r)) {
				return false
			}
		}
	}
	return true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
