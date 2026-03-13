package cuckoofilter

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/cespare/xxhash/v2"
)

const maxIndices = 2

type fingerprint uint16

func obtainsFingerprint(data []byte) fingerprint {
	value := xxhash.Sum64(data)%(math.MaxUint16-1) + 1
	if value > math.MaxUint16 {
		panic("fingerprint overflow")
	}

	return fingerprint(uint16(value))
}

func getBytesByFingerprint(fp fingerprint) []byte {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, fp); err != nil {
		return nil
	}
	return buf.Bytes()
}

func randIndices(idx1, idx2 int) int {
	rngMu.Lock()
	v := rng.Intn(maxIndices) //nolint:gosec // this is not used in a secure application
	rngMu.Unlock()
	if v == 0 {
		return idx1
	}
	return idx2
}
