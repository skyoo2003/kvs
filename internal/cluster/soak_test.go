package cluster

import (
	"flag"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/skyoo2003/kvs/internal/datadir"
)

// soakFor turns the long run on and says how long it lasts. Zero, the default, skips it, so
// `go test ./...` costs what it costs today and only `make soak` pays for this.
var soakFor = flag.Duration("soak", 0, "run the soak test for this long")

// soakKeys is a fixed set that gets overwritten rather than one that grows. A keyspace that
// grows makes the heap grow for honest reasons, and then the heap says nothing about leaks.
const soakKeys = 1000

// killEvery is how often a node is taken away. One at a time: three nodes still hold a
// majority with one gone, and taking two would measure a design decision, not a fault.
const killEvery = 30 * time.Second

// downFor is how long it stays away, and writes carry on the whole time. Restarting it straight
// away would leave it nothing to catch up on, and catching up is the part a restart has to
// survive; a run that never writes into the gap proves only that a process can be started twice.
const downFor = 10 * time.Second

// Hours of writes with a node taken away every half minute and kept away while the writes keep
// coming, held after every restart and again at the end to every value kvs acknowledged. What
// it reports - writes, restarts, heap, bytes on disk - is the record that says the thing
// survived, so those numbers go in the docs.
func TestSoakSurvivesLoadAndFailover(t *testing.T) {
	if *soakFor == 0 {
		t.Skip("soak run is off; pass -soak <duration>, or run make soak")
	}

	nodes := startCluster(t, 3)
	waitForLeader(t, nodes)

	run := &soakRun{nodes: nodes, acked: make(map[string]string, soakKeys)}
	run.load(t, *soakFor)
	run.verify(t)
	run.report(t)
}

type soakRun struct {
	nodes []*testNode
	// acked holds the last value each key was acknowledged with. A write that came back with
	// an error is a retry, not a loss, so only what returned nil is in here.
	acked map[string]string
	// last is the key of the most recent acknowledged write, used as the barrier to wait on.
	last string
	// down is the node currently stopped, if any, and backAt when it is due to come back.
	down   *testNode
	backAt time.Time
	writes int
	// shorthanded counts the acknowledged writes taken while a node was stopped. It is the
	// claim the docs make, so it is counted rather than inferred from the timings.
	shorthanded int
	refused     int
	failovers   int
	// warmup and final are the heap once the run has settled and at the end of it.
	warmup heapSample
	final  heapSample
}

// heapSample is what the run records at its endpoints. alloc is what is still reachable, which
// is the question a leak asks. inUse is what the spans hold, which is closer to what an
// operator sees and which a non-moving collector does not hand back just because objects died.
type heapSample struct {
	alloc uint64
	inUse uint64
}

func (r *soakRun) load(t *testing.T, over time.Duration) {
	t.Helper()

	start := time.Now()
	deadline := start.Add(over)
	// The heap is first read once the run has settled, so the baseline is a steady state
	// rather than a process still filling its caches.
	warmupAt := start.Add(over / 10)
	nextKill := start.Add(killEvery)

	for round := 0; time.Now().Before(deadline); round++ {
		r.put(t, "soak-"+strconv.Itoa(round%soakKeys), strconv.Itoa(round))

		now := time.Now()
		// A live process never has a zero heap, so zero is still "not read yet".
		if r.warmup.alloc == 0 && now.After(warmupAt) {
			r.warmup = heapAfterGC()
		}

		switch {
		case r.down != nil && now.After(r.backAt):
			r.bringBack(t)

		case r.down == nil && now.After(nextKill):
			r.takeDown(t, r.failovers%len(r.nodes))
			nextKill = now.Add(killEvery)
		}
	}

	if r.down != nil {
		r.bringBack(t)
	}

	r.final = heapAfterGC()
}

// put offers the write to every node until one takes it, which is what a client following a
// MOVED does. Only a write that returned nil is recorded, because that is the only one kvs
// claimed to have.
func (r *soakRun) put(t *testing.T, key, value string) {
	t.Helper()

	eventually(t, "a node to take "+key, func() bool {
		for _, node := range r.nodes {
			// A node known to be stopped is skipped rather than asked, so that a refusal means
			// what it says: the leader was somewhere else.
			if node == r.down {
				continue
			}

			if err := node.store.Put(key, value); err != nil {
				r.refused++

				continue
			}

			r.acked[key] = value
			r.last = key
			r.writes++

			if r.down != nil {
				r.shorthanded++
			}

			return true
		}

		return false
	})
}

// takeDown stops a node and leaves it stopped. The caller keeps writing, so what the cluster is
// asked to do is take writes a node short and hand them over when it comes back.
func (r *soakRun) takeDown(t *testing.T, i int) {
	t.Helper()

	r.down = r.nodes[i]
	r.down.stop(t)
	r.backAt = time.Now().Add(downFor)
}

// bringBack restarts the stopped node and checks it against every acknowledged value before the
// run is allowed to write again. Checking here rather than only at the end is what makes the
// check mean something: a value lost while the node was away would otherwise be overwritten by
// a later round, and the missing write would be gone from the evidence as well as from disk.
func (r *soakRun) bringBack(t *testing.T) {
	t.Helper()

	node := r.down
	node.restart(t)
	r.down = nil
	r.failovers++

	// A restart starts the node on an empty store, so this waits out a replay of everything,
	// not a top-up. Raft applies its log in order, so holding the last write means holding the
	// rest, and the comparison that follows is a comparison rather than a race.
	node.mustEventuallyHold(t, r.last, r.acked[r.last])
	r.mustHoldEverything(t, node)

	// Sampled every restart rather than only at the ends, because two readings cannot tell a
	// leak from a step change. No forced collection here: this is a trend, and stopping the
	// world hundreds of times would change what it is measuring.
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("restart %d of %s: heap alloc %d bytes, heap in use %d bytes, %d goroutines",
		r.failovers, node.id, stats.HeapAlloc, stats.HeapInuse, runtime.NumGoroutine())
}

// verify is what the run exists for: every value kvs acknowledged has to be on every node when
// the load stops. It is the last of the checks rather than the only one - each restart has
// already been held to the same standard against the state at that moment.
func (r *soakRun) verify(t *testing.T) {
	t.Helper()

	for _, node := range r.nodes {
		node.mustEventuallyHold(t, r.last, r.acked[r.last])
		r.mustHoldEverything(t, node)
	}
}

func (r *soakRun) mustHoldEverything(t *testing.T, node *testNode) {
	t.Helper()

	for key, want := range r.acked {
		got, err := node.store.Get(key)
		if err != nil || got != want {
			t.Errorf("%s Get(%q) = %v, %v, want %v, nil", node.id, key, got, err, want)
		}
	}
}

func (r *soakRun) report(t *testing.T) {
	t.Helper()

	t.Logf("ran %s: %d writes acknowledged over %d keys, %d refused, %d node restarts",
		*soakFor, r.writes, len(r.acked), r.refused, r.failovers)
	t.Logf("%d of those writes were taken while a node was stopped, each stopped for %s",
		r.shorthanded, downFor)
	t.Logf("heap alloc: %d bytes once settled, %d bytes at the end", r.warmup.alloc, r.final.alloc)
	t.Logf("heap in use: %d bytes once settled, %d bytes at the end, %d goroutines left",
		r.warmup.inUse, r.final.inUse, runtime.NumGoroutine())

	for _, node := range r.nodes {
		dir := filepath.Join(node.dir, datadir.RaftName)
		t.Logf("%s raft store: %d bytes", node.id, dirBytes(t, dir))
	}
}

// The heap is reported and not asserted on, which is a decision rather than an omission. Over
// four hours it went from 10MB to 139MB, and a heap profile put more than half of what was
// still reachable in Raft's own NetworkTransport - a 256KB read buffer and a 256KB write
// buffer per connection, and this test opens connections faster than a real cluster ever
// would. The only frame belonging to kvs held 544KB, which is the thousand keys themselves.
// A threshold here would either be a number picked to pass or a failure nobody can act on, so
// what stays is the measurement, and clustering.md carries what it means. The assertion that
// matters is verify: no acknowledged write went missing across 457 restarts.

func heapAfterGC() heapSample {
	// Collect first: the question is what is retained, not what has yet to be swept.
	runtime.GC()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return heapSample{alloc: stats.HeapAlloc, inUse: stats.HeapInuse}
}

func dirBytes(t *testing.T, dir string) int64 {
	t.Helper()

	var total int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	return total
}
