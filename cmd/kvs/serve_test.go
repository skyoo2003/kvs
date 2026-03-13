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
