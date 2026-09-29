package cluster

import (
	"strconv"
	"testing"
)

// BenchmarkClusterPut is one write through consensus on a three-node cluster on this machine:
// a round trip to a majority and an fsync on each of them. It is the number the durable
// single-node figure is compared against.
func BenchmarkClusterPut(b *testing.B) {
	leader := waitForLeader(b, startCluster(b, 3))

	i := 0
	for b.Loop() {
		key := "bench-" + strconv.Itoa(i%1000)
		if err := leader.store.Put(key, "value"); err != nil {
			b.Fatalf("Put(%q) error = %v", key, err)
		}
		i++
	}
}
