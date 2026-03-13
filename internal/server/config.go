// Package server provides the HTTP and gRPC server implementations for kvs.
package server

type Config struct {
	HTTPAddr string
	GRPCAddr string
}

func DefaultConfig() Config {
	return Config{
		HTTPAddr: ":3456",
		GRPCAddr: ":3457",
	}
}
