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
}

func DefaultConfig() Config {
	return Config{
		HTTPAddr: ":3456",
		GRPCAddr: ":3457",
		// Port 6379 is scanned continuously across the public internet, and kvs has no
		// authentication unless requirepass is configured. Loopback is the safe default;
		// widen it deliberately.
		RESPAddr: "127.0.0.1:6379",
	}
}
