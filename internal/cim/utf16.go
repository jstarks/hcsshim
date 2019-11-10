package cim

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// parseUtf16LE parses a UTF-16LE byte array into a string (without passing
// through a uint16 or rune array).
func parseUtf16LE(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) / 2)
	for len(b) > 0 {
		r := rune(binary.LittleEndian.Uint16(b))
		if utf16.IsSurrogate(r) && len(b) > 2 {
			sb.WriteRune(utf16.DecodeRune(r, rune(binary.LittleEndian.Uint16(b[2:]))))
			b = b[4:]
		} else {
			sb.WriteRune(r)
			b = b[2:]
		}
	}
	return sb.String()
}

// cmpcaseUtf8Utf16LE compares a UTF-8 string with a UTF-16LE encoded byte
// array, upcasing each rune through the upcase table.
func cmpcaseUtf8Utf16LE(a string, b []byte, upcase []uint16) int {
	for _, ar := range a {
		if len(b) == 0 {
			return 1
		}
		if int(ar) < len(upcase) {
			ar = rune(upcase[int(ar)])
		}
		br := rune(binary.LittleEndian.Uint16(b))
		bs := 2
		if utf16.IsSurrogate(br) {
			if len(b) == bs {
				return 1 // error?
			}
			br = utf16.DecodeRune(br, rune(binary.LittleEndian.Uint16(b[bs:])))
			if br == '\ufffd' {
				return 1 // error?
			}
			bs += 2
		} else {
			br = rune(upcase[int(br)])
		}
		if ar < br {
			return -1
		} else if ar > br {
			return 1
		}
		b = b[bs:]
	}
	if len(b) > 0 {
		return -1
	}
	return 0
}
