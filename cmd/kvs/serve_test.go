package main

import (
	"testing"

	"github.com/spf13/viper"

	"github.com/skyoo2003/kvs/internal/server"
)

func TestResolveServeConfigDefaults(t *testing.T) {
	viper.Reset()
	cmd := newServeCmd()

	got := resolveServeConfig(cmd.Flags())
	want := server.DefaultConfig()
	if got != want {
		t.Fatalf("resolveServeConfig() = %+v, want %+v", got, want)
	}
}

func TestResolveServeConfigDataDirFromFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newServeCmd()
	if err := cmd.Flags().Set("data-dir", "/var/lib/kvs"); err != nil {
		t.Fatalf("Set(\"data-dir\") error = %v", err)
	}

	if got := resolveServeConfig(cmd.Flags()).DataDir; got != "/var/lib/kvs" {
		t.Fatalf("resolveServeConfig().DataDir = %q, want %q", got, "/var/lib/kvs")
	}
}

func TestResolveServeConfigDataDirFromViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("data_dir", "/srv/kvs")

	if got := resolveServeConfig(newServeCmd().Flags()).DataDir; got != "/srv/kvs" {
		t.Fatalf("resolveServeConfig().DataDir = %q, want %q", got, "/srv/kvs")
	}
}

func TestResolveServeConfigCluster(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newServeCmd()
	for flag, value := range map[string]string{
		"raft-addr": "127.0.0.1:7901",
		"join":      "127.0.0.1:6379",
		"node-id":   "10.0.0.7:6379",
	} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatalf("Set(%q) error = %v", flag, err)
		}
	}

	got := resolveServeConfig(cmd.Flags())
	if got.RaftAddr != "127.0.0.1:7901" || got.JoinAddr != "127.0.0.1:6379" {
		t.Fatalf("resolveServeConfig() = %+v, want the raft and join addresses set", got)
	}
	if got.NodeID != "10.0.0.7:6379" {
		t.Fatalf("resolveServeConfig().NodeID = %q, want %q", got.NodeID, "10.0.0.7:6379")
	}
}

// The cluster settings have to work without flags, the way every other one does: a node in a
// cluster is likelier to be configured by file or environment than by a command line.
func TestResolveServeConfigClusterFromViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("raft_addr", "10.0.0.7:7901")
	viper.Set("join", "10.0.0.1:6379")
	viper.Set("node_id", "10.0.0.7:6379")

	got := resolveServeConfig(newServeCmd().Flags())
	if got.RaftAddr != "10.0.0.7:7901" {
		t.Errorf("resolveServeConfig().RaftAddr = %q, want %q", got.RaftAddr, "10.0.0.7:7901")
	}
	if got.JoinAddr != "10.0.0.1:6379" {
		t.Errorf("resolveServeConfig().JoinAddr = %q, want %q", got.JoinAddr, "10.0.0.1:6379")
	}
	if got.NodeID != "10.0.0.7:6379" {
		t.Errorf("resolveServeConfig().NodeID = %q, want %q", got.NodeID, "10.0.0.7:6379")
	}
}

// The environment names the documentation hands out, read through the same initConfig the binary
// runs: a documented KVS_ variable that nothing reads is a promise to nobody.
func TestResolveServeConfigFromEnvironment(t *testing.T) {
	t.Cleanup(viper.Reset)

	for name, value := range map[string]string{
		"KVS_DATA_DIR":  "/srv/kvs",
		"KVS_RAFT_ADDR": "10.0.0.7:7901",
		"KVS_JOIN":      "10.0.0.1:6379",
		"KVS_NODE_ID":   "10.0.0.7:6379",
	} {
		t.Setenv(name, value)
	}

	if err := initConfig(""); err != nil {
		t.Fatalf("initConfig() error = %v", err)
	}

	got := resolveServeConfig(newServeCmd().Flags())
	for name, pair := range map[string][2]string{
		"KVS_DATA_DIR":  {got.DataDir, "/srv/kvs"},
		"KVS_RAFT_ADDR": {got.RaftAddr, "10.0.0.7:7901"},
		"KVS_JOIN":      {got.JoinAddr, "10.0.0.1:6379"},
		"KVS_NODE_ID":   {got.NodeID, "10.0.0.7:6379"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s gave %q, want %q", name, pair[0], pair[1])
		}
	}
}

// A node is redirected to by address, so without an explicit identity it takes the one clients
// already use.
func TestResolveServeConfigNodeIDDefaultsToRESPAddr(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	got := resolveServeConfig(newServeCmd().Flags())
	if got.NodeID != got.RESPAddr {
		t.Fatalf("resolveServeConfig().NodeID = %q, want %q", got.NodeID, got.RESPAddr)
	}
}

func TestResolveServeConfigFromViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("http_addr", "127.0.0.1:8080")
	viper.Set("grpc_addr", "127.0.0.1:9090")

	cmd := newServeCmd()
	got := resolveServeConfig(cmd.Flags())
	if got.HTTPAddr != "127.0.0.1:8080" || got.GRPCAddr != "127.0.0.1:9090" {
		t.Fatalf("resolveServeConfig() = %+v, want overridden addresses", got)
	}
}

func TestResolveServeConfigFlagsOverrideViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("http_addr", "127.0.0.1:8080")
	viper.Set("grpc_addr", "127.0.0.1:9090")

	cmd := newServeCmd()
	if err := cmd.Flags().Set("http-addr", "127.0.0.1:18080"); err != nil {
		t.Fatalf("Set(http-addr) error = %v", err)
	}
	if err := cmd.Flags().Set("grpc-addr", "127.0.0.1:19090"); err != nil {
		t.Fatalf("Set(grpc-addr) error = %v", err)
	}

	got := resolveServeConfig(cmd.Flags())
	if got.HTTPAddr != "127.0.0.1:18080" || got.GRPCAddr != "127.0.0.1:19090" {
		t.Fatalf("resolveServeConfig() = %+v, want flag values", got)
	}
}
