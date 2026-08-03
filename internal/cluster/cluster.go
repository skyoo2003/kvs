// Package cluster runs a kvs node as one member of a Raft cluster, so that losing the leader
// costs an election rather than a person.
//
// It lives here rather than in the kvs package on purpose: a consensus library is a heavy thing
// to hand someone who only wanted an in-process key-value store, and the library API never
// needs it.
package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"

	"github.com/skyoo2003/kvs"
)

const (
	// applyTimeout bounds how long a write waits for the cluster to agree. Past it the caller
	// gets an error rather than a hang, and may or may not have landed — the same ambiguity any
	// timed-out write has.
	applyTimeout = 10 * time.Second

	// transportPool and transportTimeout are what Raft's own examples use.
	transportPool    = 3
	transportTimeout = 10 * time.Second

	// snapshotsRetained is how many old snapshots to keep. Two is enough to survive one that
	// turns out to be unreadable.
	snapshotsRetained = 2
)

// Config is what one node needs to stand up and be found.
type Config struct {
	// NodeID is this node's stable identity in the cluster, and by convention the address
	// clients use to reach it: that is what a node redirects a write to when it is not the
	// leader.
	NodeID string
	// RaftAddr is where the other nodes talk to this one. It is not a client address.
	RaftAddr string
	// DataDir holds the Raft log and snapshots, under a subdirectory of its own.
	DataDir string
	// Bootstrap starts a brand new cluster of one, which exactly one node in a new deployment
	// should do. A node that already has a log already has its cluster and ignores this.
	Bootstrap bool
	// LogOutput is where the consensus library writes. Nil means standard error; a test that
	// stands up three nodes points it somewhere quieter.
	LogOutput io.Writer
}

func (c Config) logOutput() io.Writer {
	if c.LogOutput == nil {
		return os.Stderr
	}

	return c.LogOutput
}

// Node is this process's membership in the cluster.
type Node struct {
	raft      *raft.Raft
	store     *kvs.Store
	transport *raft.NetworkTransport
	logs      *raftboltdb.BoltStore
	// id is this node's own identity, kept so that it can leave itself out when counting the
	// others.
	id string

	// applyMu serializes the two halves of a write. Working out what a transaction would change
	// and getting the cluster to agree to it have to happen as one step, or two writes computed
	// against the same state would both commit and the second would quietly undo the first.
	//
	// ponytail: this makes writes strictly one at a time per leader, each paying a full
	// consensus round. Batching several transactions into one round is the upgrade path; the
	// milestone here is that writes come back at all, not that they come back fast.
	applyMu sync.Mutex
}

// Start brings this node up and, if asked, starts a new cluster around it.
func Start(cfg Config, store *kvs.Store) (*Node, error) {
	dir := filepath.Join(cfg.DataDir, "raft")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create raft dir: %w", err)
	}

	advertise, err := net.ResolveTCPAddr("tcp", cfg.RaftAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve raft address %s: %w", cfg.RaftAddr, err)
	}

	output := cfg.logOutput()

	transport, err := raft.NewTCPTransport(cfg.RaftAddr, advertise, transportPool, transportTimeout, output)
	if err != nil {
		return nil, fmt.Errorf("listen raft %s: %w", cfg.RaftAddr, err)
	}

	snapshots, err := raft.NewFileSnapshotStore(dir, snapshotsRetained, output)
	if err != nil {
		_ = transport.Close()

		return nil, fmt.Errorf("open raft snapshots: %w", err)
	}

	logs, err := raftboltdb.NewBoltStore(filepath.Join(dir, "raft.db"))
	if err != nil {
		_ = transport.Close()

		return nil, fmt.Errorf("open raft log: %w", err)
	}

	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(cfg.NodeID)
	config.Logger = hclog.New(&hclog.LoggerOptions{Name: "raft", Output: output, Level: hclog.Warn})

	consensus, err := raft.NewRaft(config, &fsm{store: store}, logs, logs, snapshots, transport)
	if err != nil {
		_ = logs.Close()
		_ = transport.Close()

		return nil, fmt.Errorf("start raft: %w", err)
	}

	node := &Node{raft: consensus, store: store, transport: transport, logs: logs, id: cfg.NodeID}

	if err := node.bootstrapIfAsked(cfg, config.LocalID, logs, snapshots); err != nil {
		_ = node.Close()

		return nil, err
	}

	// Every change now arrives through consensus, including on the leader.
	store.SetReplicator(node.Write)

	return node, nil
}

// bootstrapIfAsked starts a cluster of one, but only for a node that has no log yet. A node that
// already has one already belongs to a cluster, and bootstrapping it again would invent a second
// one alongside the first.
func (n *Node) bootstrapIfAsked(
	cfg Config, id raft.ServerID, logs raft.LogStore, snapshots raft.SnapshotStore,
) error {
	if !cfg.Bootstrap {
		return nil
	}

	stable, ok := logs.(raft.StableStore)
	if !ok {
		return errors.New("raft log store cannot hold stable state")
	}

	existing, err := raft.HasExistingState(logs, stable, snapshots)
	if err != nil {
		return fmt.Errorf("read raft state: %w", err)
	}
	if existing {
		return nil
	}

	future := n.raft.BootstrapCluster(raft.Configuration{Servers: []raft.Server{{
		ID:      id,
		Address: n.transport.LocalAddr(),
	}}})
	if err := future.Error(); err != nil {
		return fmt.Errorf("bootstrap cluster: %w", err)
	}

	return nil
}

// Write runs fn on the leader, gets the cluster to agree on what it would change, and only then
// lets any node change anything.
//
// The order is the reason Speculate exists: applying first and replicating after would leave a
// leader that lost an election holding writes nobody else has.
func (n *Node) Write(fn func(tx *kvs.Tx) error) error {
	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	if n.raft.State() != raft.Leader {
		return &kvs.NotLeaderError{Leader: n.LeaderID()}
	}

	lines, err := n.store.Speculate(fn)
	if err != nil {
		return err
	}
	// A transaction that changed nothing has nothing to agree on.
	if len(lines) == 0 {
		return nil
	}

	payload, err := json.Marshal(lines)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}

	future := n.raft.Apply(payload, applyTimeout)
	if err := future.Error(); err != nil {
		if errors.Is(err, raft.ErrLeadershipLost) || errors.Is(err, raft.ErrNotLeader) {
			return &kvs.NotLeaderError{Leader: n.LeaderID()}
		}

		return fmt.Errorf("replicate write: %w", err)
	}

	// The FSM reports what applying the frame did, which is where a decode failure surfaces.
	if applyErr, ok := future.Response().(error); ok && applyErr != nil {
		return applyErr
	}

	return nil
}

// Join adds another node as a voting member. Only the leader can, so a node that is asked and is
// not the leader says where to go instead.
func (n *Node) Join(id, addr string) error {
	if n.raft.State() != raft.Leader {
		return &kvs.NotLeaderError{Leader: n.LeaderID()}
	}

	// Re-adding a member that is already there is how a restarted node rejoins, so it is not
	// treated as a mistake.
	future := n.raft.AddVoter(raft.ServerID(id), raft.ServerAddress(addr), 0, applyTimeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("add %s to the cluster: %w", id, err)
	}

	return nil
}

func (n *Node) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

// Peers is how many nodes other than this one are voting members. It comes from the agreed
// configuration rather than from live connections, so a member that is currently down still
// counts: it is expected back, and a count that dropped when a node went quiet would report the
// cluster as smaller than the majority is calculated against.
func (n *Node) Peers() int {
	future := n.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return 0
	}

	peers := 0
	for _, server := range future.Configuration().Servers {
		if string(server.ID) != n.id {
			peers++
		}
	}

	return peers
}

// LeaderID is the current leader's node ID, which by convention is the address clients use to
// reach it. It is empty while an election is in progress.
func (n *Node) LeaderID() string {
	_, id := n.raft.LeaderWithID()

	return string(id)
}

// Close releases what this node holds. It does not remove the node from the cluster's
// configuration: a node that goes down is meant to come back and rejoin.
func (n *Node) Close() error {
	n.store.SetReplicator(nil)

	if err := n.raft.Shutdown().Error(); err != nil {
		return fmt.Errorf("shut down raft: %w", err)
	}
	if err := n.transport.Close(); err != nil {
		return fmt.Errorf("close raft transport: %w", err)
	}
	if err := n.logs.Close(); err != nil {
		return fmt.Errorf("close raft log: %w", err)
	}

	return nil
}

// fsm is the store seen the way Raft needs to see it. The three methods it has to provide are
// the three the store already grew for its own log and for replication.
type fsm struct {
	store *kvs.Store
}

func (f *fsm) Apply(entry *raft.Log) interface{} {
	var lines [][]byte
	if err := json.Unmarshal(entry.Data, &lines); err != nil {
		return fmt.Errorf("decode frame: %w", err)
	}

	if err := f.store.ApplyReplicated(lines); err != nil {
		return err
	}

	return nil
}

func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	lines, err := f.store.Snapshot()
	if err != nil {
		return nil, err
	}

	return &snapshot{lines: lines}, nil
}

func (f *fsm) Restore(reader io.ReadCloser) error {
	defer func() { _ = reader.Close() }()

	var lines [][]byte
	if err := json.NewDecoder(reader).Decode(&lines); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}

	return f.store.ReplaceWith(lines)
}

// snapshot is the keyspace as it stood when Raft asked, already encoded. Holding the encoded
// form rather than the store is what lets Persist run without holding up writes.
type snapshot struct {
	lines [][]byte
}

func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	if err := json.NewEncoder(sink).Encode(s.lines); err != nil {
		_ = sink.Cancel()

		return fmt.Errorf("write snapshot: %w", err)
	}

	if err := sink.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}

	return nil
}

func (s *snapshot) Release() {}
