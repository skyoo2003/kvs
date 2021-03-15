package cuckoofilter

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"

	"github.com/cespare/xxhash/v2"
)

const maxIndices = 2

type fingerprint uint16

func obtainsFingerprint(data []byte) fingerprint {
	return fingerprint(xxhash.Sum64(data)%(math.MaxUint16-1) + 1)
}

func getBytesByFingerprint(fp fingerprint) []byte {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, fp); err != nil {
		return nil
	}
	return buf.Bytes()
}

func randIndices(idx1, idx2 int) int {
	// nolint:gosec
	if v := rand.Intn(maxIndices); v == 0 {
		return idx1
	}
	return idx2
}
