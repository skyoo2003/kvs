package cluster

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/skyoo2003/kvs"
)

// The milestone in one test: the leader goes away, nobody does anything, and writes come back.
func TestWritesResumeAfterLeaderDies(t *testing.T) {
	nodes := startCluster(t, 3)

	leader := waitForLeader(t, nodes)
	if err := leader.store.Put("greeting", "hello"); err != nil {
		t.Fatalf("Put() on the leader error = %v", err)
	}

	// Everyone has it, not just the node that took it.
	for _, node := range nodes {
		node.mustEventuallyHold(t, "greeting", "hello")
	}

	leader.stop(t)
	survivors := without(nodes, leader)

	// Nobody intervenes here. That is the point. The clock runs from the leader going away to a
	// write being accepted again, because that gap is the one a client feels, and the number the
	// documentation quotes has to come from somewhere.
	start := time.Now()
	next := waitForWrite(t, survivors, "after", "failover")
	t.Logf("writes resumed at %s after %s", next.id, time.Since(start).Round(time.Millisecond))

	// The write from before the failover is still there.
	if got, err := next.store.Get("greeting"); err != nil || got != "hello" {
		t.Fatalf("Get() after failover = %v, %v, want %v, nil", got, err, "hello")
	}

	for _, node := range survivors {
		node.mustEventuallyHold(t, "after", "failover")
	}
}

func TestOnlyTheLeaderTakesWrites(t *testing.T) {
	nodes := startCluster(t, 3)
	leader := waitForLeader(t, nodes)

	for _, node := range without(nodes, leader) {
		err := node.store.Put("greeting", "hello")
		if !errors.Is(err, kvs.ErrNotLeader) {
			t.Fatalf("Put() on a follower error = %v, want %v", err, kvs.ErrNotLeader)
		}

		var notLeader *kvs.NotLeaderError
		if !errors.As(err, &notLeader) || notLeader.Leader != leader.id {
			t.Fatalf("follower pointed at %v, want %s", notLeader, leader.id)
		}

		// Reads are fine on any node.
		if _, err := node.store.Get("greeting"); !errors.Is(err, kvs.ErrKeyNotFound) {
			t.Fatalf("Get() on a follower error = %v, want %v", err, kvs.ErrKeyNotFound)
		}
	}
}

// A node that was away has to come back knowing what it missed.
func TestRestartedNodeCatchesUp(t *testing.T) {
	nodes := startCluster(t, 3)
	leader := waitForLeader(t, nodes)

	away := without(nodes, leader)[0]
	away.stop(t)

	if err := leader.store.Put("while-away", "written"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	away.restart(t)
	away.mustEventuallyHold(t, "while-away", "written")
}

// What Snapshot writes, Restore has to read: a node that joins late is rebuilt from exactly
// these bytes.
func TestFSMSnapshotRoundTrip(t *testing.T) {
	source := newStore()
	if err := source.Put("greeting", "hello"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	snap, err := (&fsm{store: source}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	sink := &memorySink{}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}
	snap.Release()

	target := newStore()
	if err := (&fsm{store: target}).Restore(io.NopCloser(sink)); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	if got, err := target.Get("greeting"); err != nil || got != "hello" {
		t.Fatalf("Get() after restore = %v, %v, want %v, nil", got, err, "hello")
	}
}

// A cluster of one is still a cluster, and is what the first node of a new deployment is until
// the others arrive.
func TestSingleNodeClusterTakesWrites(t *testing.T) {
	nodes := startCluster(t, 1)
	leader := waitForLeader(t, nodes)

	if err := leader.store.Put("greeting", "hello"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got, err := leader.store.Get("greeting"); err != nil || got != "hello" {
		t.Fatalf("Get() = %v, %v, want %v, nil", got, err, "hello")
	}
}

// testNode is one member, kept alongside what a test needs to stop it and start it again.
type testNode struct {
	id       string
	raftAddr string
	dir      string
	store    *kvs.Store
	node     *Node
	stopped  bool
}

func newStore() *kvs.Store {
	store := kvs.NewStore()
	store.SetCodec(kvs.StringCodec{})

	return store
}

func startCluster(t *testing.T, size int) []*testNode {
	t.Helper()

	nodes := make([]*testNode, 0, size)
	for i := range size {
		node := &testNode{
			id:       "node-" + strconv.Itoa(i),
			raftAddr: reserveAddr(t),
			dir:      filepath.Join(t.TempDir(), "node-"+strconv.Itoa(i)),
		}
		node.start(t, i == 0)
		nodes = append(nodes, node)
	}

	// Only the leader can admit anyone, so the first node has to win its election first.
	if size > 1 {
		leader := waitForLeader(t, nodes[:1])
		for _, node := range nodes[1:] {
			if err := leader.node.Join(node.id, node.raftAddr); err != nil {
				t.Fatalf("Join(%s) error = %v", node.id, err)
			}
		}
	}

	return nodes
}

func (n *testNode) start(t *testing.T, bootstrap bool) {
	t.Helper()

	n.store = newStore()

	node, err := Start(Config{
		NodeID:    n.id,
		RaftAddr:  n.raftAddr,
		DataDir:   n.dir,
		Bootstrap: bootstrap,
		LogOutput: io.Discard,
	}, n.store)
	if err != nil {
		t.Fatalf("Start(%s) error = %v", n.id, err)
	}

	n.node, n.stopped = node, false
	t.Cleanup(func() {
		if !n.stopped {
			_ = n.node.Close()
		}
	})
}

func (n *testNode) stop(t *testing.T) {
	t.Helper()

	if err := n.node.Close(); err != nil {
		t.Fatalf("Close(%s) error = %v", n.id, err)
	}
	n.stopped = true
}

// restart brings the node back on the same address with the same log, which is what a process
// that was killed and started again looks like to the rest of the cluster.
func (n *testNode) restart(t *testing.T) {
	t.Helper()

	n.start(t, false)
}

func (n *testNode) mustEventuallyHold(t *testing.T, key, want string) {
	t.Helper()

	eventually(t, n.id+" to hold "+key, func() bool {
		got, err := n.store.Get(key)

		return err == nil && got == want
	})
}

func waitForLeader(t *testing.T, nodes []*testNode) *testNode {
	t.Helper()

	var leader *testNode
	eventually(t, "a leader to be elected", func() bool {
		for _, node := range nodes {
			if node.node.IsLeader() {
				leader = node

				return true
			}
		}

		return false
	})

	return leader
}

// waitForWrite offers the write to every node until one takes it, and reports which did. It is
// what a client retrying a rejected write does, so timing it measures the outage a client sees
// rather than the moment the cluster privately agreed on a leader.
func waitForWrite(t *testing.T, nodes []*testNode, key, value string) *testNode {
	t.Helper()

	var taken *testNode
	eventually(t, "a write to be accepted again", func() bool {
		for _, node := range nodes {
			if err := node.store.Put(key, value); err == nil {
				taken = node

				return true
			}
		}

		return false
	})

	return taken
}

func without(nodes []*testNode, excluded *testNode) []*testNode {
	rest := make([]*testNode, 0, len(nodes)-1)
	for _, node := range nodes {
		if node != excluded {
			rest = append(rest, node)
		}
	}

	return rest
}

// reserveAddr picks a free port and lets go of it, so Raft can bind it and a restarted node can
// bind it again.
func reserveAddr(t *testing.T) string {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := listener.Addr().String()

	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	return addr
}

// eventually waits for consensus to settle. Elections take as long as they take, so polling is
// the only honest way to wait for one.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// memorySink stands in for Raft's snapshot sink so the round trip can be checked without a
// cluster.
type memorySink struct {
	data []byte
	read int
}

func (s *memorySink) Write(p []byte) (int, error) {
	s.data = append(s.data, p...)

	return len(p), nil
}

func (s *memorySink) Read(p []byte) (int, error) {
	if s.read >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.read:])
	s.read += n

	return n, nil
}

func (s *memorySink) Close() error  { return nil }
func (s *memorySink) ID() string    { return "memory" }
func (s *memorySink) Cancel() error { return nil }
