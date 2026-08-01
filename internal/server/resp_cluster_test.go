package server

import (
	"slices"
	"strings"
	"testing"
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
