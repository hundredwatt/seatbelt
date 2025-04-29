package postgres2

import (
	"encoding/binary"
	"math/bits"
)

// rotl32 performs a left rotation on a 32-bit integer.
func rotl32(x uint32, k uint) uint32 {
	return bits.RotateLeft32(x, int(k))
}

// mix performs the mixing step of the hash algorithm.
func mix(a, b, c *uint32) {
	*a -= *c
	*a ^= rotl32(*c, 4)
	*c += *b
	*b -= *a
	*b ^= rotl32(*a, 6)
	*a += *c
	*c -= *b
	*c ^= rotl32(*b, 8)
	*b += *a
	*a -= *c
	*a ^= rotl32(*c, 16)
	*c += *b
	*b -= *a
	*b ^= rotl32(*a, 19)
	*a += *c
	*c -= *b
	*c ^= rotl32(*b, 4)
	*b += *a
}

// final performs the final mixing step.
func final(a, b, c *uint32) {
	*c ^= *b
	*c -= rotl32(*b, 14)
	*a ^= *c
	*a -= rotl32(*c, 11)
	*b ^= *a
	*b -= rotl32(*a, 25)
	*c ^= *b
	*c -= rotl32(*b, 16)
	*a ^= *c
	*a -= rotl32(*c, 4)
	*b ^= *a
	*b -= rotl32(*a, 14)
	*c ^= *b
	*c -= rotl32(*b, 24)
}

// PostgresHashtextextend mimics the hashtextextended function in PostgreSQL.
// It implements the logic from hash_bytes_extended in src/common/hashfn.c
func PostgresHashtextextended(text string, seed int64) int64 {
	// k := *(*[]byte)(unsafe.Pointer(&text)) // Get bytes without allocation (Original unsafe way)
	k := []byte(text) // Use standard Go conversion (safer, involves allocation)
	length := len(k)

	// Initialize internals
	var a, b, c uint32
	initval := uint32(0x9e3779b9) // Golden ratio conjugate
	a = initval + uint32(length) + 3923095
	b = a
	c = a

	// Handle the seed
	if seed != 0 {
		// Note: Shifting int64 in Go performs sign extension if the number is negative.
		// Casting to uint64 first ensures zero extension, matching the C behavior
		// where the seed is treated as uint64 before splitting.
		useed := uint64(seed)
		a += uint32(useed >> 32)
		b += uint32(useed)
		mix(&a, &b, &c)
	}

	// Process the key in 12-byte chunks
	p := 0
	for length >= 12 {
		// Use LittleEndian matching the !WORDS_BIGENDIAN path in hashfn.c
		a += binary.LittleEndian.Uint32(k[p:])
		b += binary.LittleEndian.Uint32(k[p+4:])
		c += binary.LittleEndian.Uint32(k[p+8:])
		mix(&a, &b, &c)
		p += 12
		length -= 12
	}

	// Handle the last 11 bytes (Little Endian)
	switch length {
	case 11:
		c += uint32(k[p+10]) << 24
		fallthrough
	case 10:
		c += uint32(k[p+9]) << 16
		fallthrough
	case 9:
		c += uint32(k[p+8]) << 8
		fallthrough
		// the lowest byte of c is reserved for the length
	case 8:
		b += uint32(k[p+7]) << 24
		fallthrough
	case 7:
		b += uint32(k[p+6]) << 16
		fallthrough
	case 6:
		b += uint32(k[p+5]) << 8
		fallthrough
	case 5:
		b += uint32(k[p+4])
		fallthrough
	case 4:
		a += uint32(k[p+3]) << 24
		fallthrough
	case 3:
		a += uint32(k[p+2]) << 16
		fallthrough
	case 2:
		a += uint32(k[p+1]) << 8
		fallthrough
	case 1:
		a += uint32(k[p])
		// case 0: nothing left to add
	}

	final(&a, &b, &c)

	// Combine b and c for the 64-bit result
	result := (uint64(b) << 32) | uint64(c)
	return int64(result) // Cast to int64 to match Go's int type and test cases
}
