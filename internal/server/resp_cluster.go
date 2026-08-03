package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/skyoo2003/kvs"
)

const (
	// respCmdJoin asks the node it is sent to, which has to be the leader, to admit another
	// node: KVS.JOIN <node-id> <raft-addr>. Reusing the RESP listener means no second port and
	// no second way to authenticate.
	respCmdJoin = "KVS.JOIN"

	// respJoinRetry is how long a starting node waits before asking again. A cluster that has
	// not elected anyone yet is the usual reason to be turned away.
	respJoinRetry = time.Second
)

// clusterNode is the part of a cluster membership this package uses. Naming it here rather than
// taking the concrete type keeps the RESP server testable without standing up consensus, and
// keeps the dependency pointing one way.
type clusterNode interface {
	Join(id, addr string) error
	LeaderID() string
	IsLeader() bool
	Peers() int
}

func respClusterCommands() map[string]respCommand {
	return map[string]respCommand{
		respCmdJoin: {run: (*respConn).cmdJoin, minArgs: 3, maxArgs: 3},
	}
}

func (c *respConn) cmdJoin(args [][]byte) error {
	if c.server.cluster == nil {
		return c.writer.WriteError("ERR this node is not running in a cluster")
	}

	if err := c.server.cluster.Join(string(args[1]), string(args[2])); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteSimple("OK")
}

// replicationRole is what this node is to a client asking INFO or HELLO: the node that takes
// writes, or one that will only point at it. A node outside a cluster is always the former,
// which is what a standalone Redis reports too.
func (s *RESPServer) replicationRole() string {
	if s.cluster == nil || s.cluster.IsLeader() {
		return respRoleMaster
	}

	return respRoleSlave
}

// replicationInfo is the INFO section describing where writes go. Redis fills these fields from
// its own replication, and a clustered kvs node has the same two facts to report — whether it
// takes writes, and who does if it does not — so they are answered rather than left at the
// standalone defaults, which would tell a follower's client it was talking to the primary.
//
// master_link_status is deliberately absent: kvs does not track how far behind a follower is, and
// a field invented to look complete is the thing this reporting exists to avoid.
func (s *RESPServer) replicationInfo() []string {
	lines := []string{"# Replication", "role:" + s.replicationRole()}

	switch {
	case s.cluster == nil:
		lines = append(lines, "connected_slaves:0")
	case s.cluster.IsLeader():
		lines = append(lines, "connected_slaves:"+strconv.Itoa(s.cluster.Peers()))
	default:
		// A follower has no followers of its own; kvs does not chain them.
		lines = append(lines, "connected_slaves:0")
		// Only when the leader is named by address, which is the convention but not a rule: a
		// node id may be any stable string, and half a host:port pair helps nobody.
		if host, port, err := net.SplitHostPort(s.cluster.LeaderID()); err == nil {
			lines = append(lines, "master_host:"+host, "master_port:"+port)
		}
	}

	return lines
}

// movedTo is the reply that sends a client to the leader. It is Redis Cluster's wording because
// clients already know how to follow it, and the slot is always zero because kvs does not shard.
func movedTo(err error) string {
	var notLeader *kvs.NotLeaderError
	if !errors.As(err, &notLeader) || notLeader.Leader == "" {
		return "CLUSTERDOWN No leader has been elected yet"
	}

	return fmt.Sprintf("MOVED 0 %s", notLeader.Leader)
}

// joinCluster keeps asking the node at addr to admit this one until it works or ctx ends.
//
// Like everything else here that reaches for another node at startup, failing to get in is a log
// line rather than a failed boot: the process still answers, and the cluster is something it
// keeps trying to reach.
func joinCluster(ctx context.Context, addr, password, nodeID, raftAddr string) {
	for {
		err := requestJoin(ctx, addr, password, nodeID, raftAddr)
		if err == nil {
			log.Printf("kvs: joined the cluster at %s as %s", addr, nodeID)

			return
		}
		if ctx.Err() != nil {
			return
		}

		log.Printf("kvs: could not join the cluster at %s: %v; trying again in %s", addr, err, respJoinRetry)

		select {
		case <-ctx.Done():
			return
		case <-time.After(respJoinRetry):
		}
	}
}

func requestJoin(ctx context.Context, addr, password, nodeID, raftAddr string) error {
	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	peer := newPeerConn(conn)

	if password != "" {
		if err := peer.send("AUTH", password); err != nil {
			return err
		}
		if err := peer.expectOK("AUTH"); err != nil {
			return err
		}
	}

	if err := peer.send(respCmdJoin, nodeID, raftAddr); err != nil {
		return err
	}

	return peer.expectOK(respCmdJoin)
}
