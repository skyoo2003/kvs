package server

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/skyoo2003/kvs"
)

// fakeCluster stands in for a real membership so that what a node reports about itself can be
// checked without standing up consensus.
type fakeCluster struct {
	leader   bool
	leaderID string
	peers    int
}

func (f *fakeCluster) Join(string, string) error { return nil }
func (f *fakeCluster) LeaderID() string          { return f.leaderID }
func (f *fakeCluster) IsLeader() bool            { return f.leader }
func (f *fakeCluster) Peers() int                { return f.peers }

// A node answering INFO has to say which node it actually is. The standalone defaults told a
// follower's client it was talking to the one that takes writes.
func TestReplicationInfoReportsWhatThisNodeIs(t *testing.T) {
	tests := []struct {
		name    string
		cluster clusterNode
		want    []string
		absent  []string
	}{
		{
			name:   "outside a cluster nothing has changed",
			want:   []string{"role:" + respRoleMaster, "connected_slaves:0"},
			absent: []string{"master_host:"},
		},
		{
			name:    "a leader counts the others",
			cluster: &fakeCluster{leader: true, leaderID: "127.0.0.1:6381", peers: 2},
			want:    []string{"role:" + respRoleMaster, "connected_slaves:2"},
			absent:  []string{"master_host:"},
		},
		{
			name:    "a follower names the leader",
			cluster: &fakeCluster{leaderID: "127.0.0.1:6381", peers: 2},
			want: []string{
				"role:" + respRoleSlave,
				"connected_slaves:0",
				"master_host:127.0.0.1",
				"master_port:6381",
			},
		},
		{
			// A node id is any stable string; half a host:port pair helps nobody.
			name:    "a leader known by name only is not split into an address",
			cluster: &fakeCluster{leaderID: "node-1"},
			want:    []string{"role:" + respRoleSlave},
			absent:  []string{"master_host:", "master_port:"},
		},
		{
			name:    "during an election there is no leader to name",
			cluster: &fakeCluster{},
			want:    []string{"role:" + respRoleSlave},
			absent:  []string{"master_host:"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := NewRESPServer(nil, "")
			if test.cluster != nil {
				server.SetCluster(test.cluster)
			}

			lines := server.replicationInfo()
			if lines[0] != "# Replication" {
				t.Fatalf("first line = %q, want the section header", lines[0])
			}

			for _, want := range test.want {
				if !slices.Contains(lines, want) {
					t.Errorf("replicationInfo() = %v, want it to contain %q", lines, want)
				}
			}
			for _, absent := range test.absent {
				for _, line := range lines {
					if strings.HasPrefix(line, absent) {
						t.Errorf("replicationInfo() = %v, want no %q line", lines, absent)
					}
				}
			}
		})
	}
}

// The redirect is the whole client-facing contract of a cluster: a write that lands on a follower
// has to come back with somewhere else to take it. Redis Cluster's wording is used verbatim so
// that clients already know how to follow it, which is what makes the exact string worth pinning.
func TestMovedToSendsClientsToTheLeader(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a known leader is named",
			err:  &kvs.NotLeaderError{Leader: "127.0.0.1:6381"},
			want: "MOVED 0 127.0.0.1:6381",
		},
		{
			// Slot zero always: there is nothing sharded, so there is no other slot to name.
			name: "another leader, the same slot",
			err:  &kvs.NotLeaderError{Leader: "10.0.0.7:6379"},
			want: "MOVED 0 10.0.0.7:6379",
		},
		{
			name: "mid-election there is nobody to name",
			err:  &kvs.NotLeaderError{},
			want: "CLUSTERDOWN No leader has been elected yet",
		},
		{
			// Nothing else gets dressed up as a redirect that would send a client somewhere.
			name: "an unrelated error is not a redirect",
			err:  errors.New("something else"),
			want: "CLUSTERDOWN No leader has been elected yet",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := movedTo(test.err); got != test.want {
				t.Fatalf("movedTo(%v) = %q, want %q", test.err, got, test.want)
			}
		})
	}
}

// Every command reports its errors through writeFailure, so the redirect has to survive that trip
// rather than reaching the client as a complaint about its input.
func TestWriteFailureRedirectsRatherThanBlamingTheClient(t *testing.T) {
	store := kvs.NewStore()
	store.SetCodec(kvs.StringCodec{})
	// What a follower does with a write: hand it to consensus, which says take it elsewhere.
	store.SetReplicator(func(func(*kvs.Tx) error) error {
		return &kvs.NotLeaderError{Leader: "127.0.0.1:6381"}
	})

	client := newRESPClient(t, store)

	client.do("-MOVED 0 127.0.0.1:6381"+respCRLF, "SET", "greeting", "hello")
	client.do("-MOVED 0 127.0.0.1:6381"+respCRLF, "DEL", "greeting")
	// Reads are not redirected: any node answers them.
	client.do("$-1"+respCRLF, "GET", "greeting")
}

// HELLO carries the same role, and a client reading one and not the other would get two answers
// to the same question.
func TestHelloRoleMatchesInfo(t *testing.T) {
	server := NewRESPServer(nil, "")
	server.SetCluster(&fakeCluster{leaderID: "127.0.0.1:6381"})

	if got := server.replicationRole(); got != respRoleSlave {
		t.Fatalf("replicationRole() on a follower = %q, want %q", got, respRoleSlave)
	}
	if !slices.Contains(server.replicationInfo(), "role:"+server.replicationRole()) {
		t.Error("INFO and HELLO disagree about this node's role")
	}
}
