package kvs

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// soakFor turns the long run on and says how long it lasts. Zero, the default, skips it.
var soakFor = flag.Duration("soak", 0, "run the soak test for this long")

// soakKeys is a fixed set that gets overwritten, so the keyspace stays the same size while the
// log does not. That difference is the measurement.
const soakKeys = 1000

// soakSizeCap stops the run before it can fill the disk it is measuring. Reaching it is a
// result rather than a failure: it says how long a process writing this fast has before the
// log is that large. It assumes the temporary directory has that much to spare - a smaller
// filesystem fails the write instead, which says so plainly enough.
const soakSizeCap = 2 << 30

// soakSizeEvery is how many writes pass between size checks. Statting the log on every write
// would measure the stat.
const soakSizeEvery = 10000

// The append log is compacted when Open replays it and nowhere else, so a process that stays
// up keeps appending - clustering.md says so, and this puts a number on it. A rate is more use
// to whoever has to size a disk than the sentence alone. Changing that behavior is not v1
// work; measuring it is.
func TestSoakMeasuresLogGrowth(t *testing.T) {
	if *soakFor == 0 {
		t.Skip("soak run is off; pass -soak <duration>, or run make soak")
	}

	dir := t.TempDir()
	store := openTestStore(t, dir)

	start := time.Now()
	deadline := start.Add(*soakFor)
	// The heap is first read once the run has settled, so the baseline is a steady state
	// rather than a process still filling its caches.
	warmupAt := start.Add(*soakFor / 10)

	var writes int
	var warmup uint64
	var capped bool

	for ; time.Now().Before(deadline); writes++ {
		key := "soak-" + strconv.Itoa(writes%soakKeys)
		if err := store.Put(key, strconv.Itoa(writes)); err != nil {
			t.Fatalf("Put(%q) error = %v", key, err)
		}

		now := time.Now()
		// A live process never has a zero heap, so zero is still "not read yet".
		if warmup == 0 && now.After(warmupAt) {
			warmup = heapInUse()
		}

		if writes%soakSizeEvery == 0 && logBytes(t, dir) > soakSizeCap {
			capped = true

			break
		}
	}

	reportGrowth(t, dir, writes, time.Since(start), capped)
	assertHeapSettled(t, warmup, heapInUse())
}

func reportGrowth(t *testing.T, dir string, writes int, elapsed time.Duration, capped bool) {
	t.Helper()

	size := logBytes(t, dir)
	if capped {
		t.Logf("stopped early at the %d byte cap, %s into a %s run", soakSizeCap, elapsed, *soakFor)
	}

	t.Logf("ran %s: %d writes over %d keys, log is %d bytes (%.1f bytes per write)",
		elapsed.Round(time.Second), writes, soakKeys, size, float64(size)/float64(writes))
	t.Logf("log growth: %.1f MiB/hour at this write rate", float64(size)/(1<<20)/elapsed.Hours())
}

// assertHeapSettled is the only assertion here. The log is expected to grow; the keyspace is
// not, so what holds it has to settle. Half again is the loosest reading that still catches
// something holding on to every write.
func assertHeapSettled(t *testing.T, warmup, final uint64) {
	t.Helper()

	// A run that hit the size cap before the baseline was due has nothing to compare, and a
	// baseline of zero would turn the result the cap exists to produce into a failure.
	if warmup == 0 {
		t.Logf("heap in use: %d bytes at the end, with no settled reading to compare it to", final)

		return
	}

	t.Logf("heap in use: %d bytes once settled, %d bytes at the end", warmup, final)

	if limit := warmup + warmup/2; final > limit {
		t.Errorf("heap in use went from %d to %d bytes, want at most %d", warmup, final, limit)
	}
}

func heapInUse() uint64 {
	// Collect first: the question is what is retained, not what has yet to be swept.
	runtime.GC()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return stats.HeapInuse
}

func logBytes(t *testing.T, dir string) int64 {
	t.Helper()

	info, err := os.Stat(filepath.Join(dir, logName))
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}

	return info.Size()
}
