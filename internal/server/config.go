// Package server provides the HTTP, gRPC, and RESP server implementations for kvs.
package server

type Config struct {
	HTTPAddr string
	GRPCAddr string
	// RESPAddr is where Redis and Valkey clients connect. An empty value disables the
	// protocol.
	RESPAddr string
	// RESPPassword is the credential RESP clients must send with AUTH. An empty value
	// leaves the listener open, which is why RESPAddr defaults to loopback.
	//
	// There is deliberately no command line flag for this: an argument is visible to
	// anything that can list processes. It comes from the config file or from
	// KVS_RESP_PASSWORD instead.
	RESPPassword string
	// DataDir is where the append log lives, and setting it is what makes the keyspace
	// outlive the process. An empty value keeps everything in memory.
	//
	// Off by default because turning it on means writing files somewhere nobody named, and
	// because starting empty is what kvs has always done.
	DataDir string
	// RaftAddr is where the other nodes of the cluster talk to this one. Setting it is what
	// puts the node in a cluster: every write then has to pass consensus, and losing the leader
	// costs an election rather than a person.
	//
	// Empty means a single node, which is what kvs has always been and still is by default.
	RaftAddr string
	// JoinAddr is the RESP address of a node already in the cluster. Empty on the first node,
	// which starts the cluster; set on every node after it.
	JoinAddr string
	// NodeID is this node's stable identity, and the address a client is redirected to when it
	// reaches the wrong node. It defaults to RESPAddr for that reason.
	NodeID string
}

func DefaultConfig() Config {
	cfg := Config{
		HTTPAddr: ":3456",
		GRPCAddr: ":3457",
		// Port 6379 is scanned continuously across the public internet, and kvs has no
		// authentication unless requirepass is configured. Loopback is the safe default;
		// widen it deliberately.
		RESPAddr: "127.0.0.1:6379",
	}
	// A node is redirected to by address, so its identity follows the one clients use.
	cfg.NodeID = cfg.RESPAddr

	return cfg
}
