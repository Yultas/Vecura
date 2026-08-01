package db

import (
	"encoding/binary"
	"math"
	"unsafe"
)

// float32ToBlob encodes a []float32 into a little-endian byte slice for storage.
func float32ToBlob(vec []float32) []byte {
	blob := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(v))
	}
	return blob
}

// blobToFloat32 decodes a BLOB into a []float32 without copying the
// underlying bytes (zero-copy via unsafe.Slice). The returned slice shares
// memory with blob and must not be mutated.
func blobToFloat32(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	n := len(blob) / 4
	if n == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&blob[0])), n)
}
