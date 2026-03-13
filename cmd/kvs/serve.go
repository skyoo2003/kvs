package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/internal/server"
)

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "serve",
		Short:         "Run the HTTP and gRPC servers",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := resolveServeConfig(cmd.Flags())

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return server.Run(ctx, cfg, kvs.NewStore())
		},
	}

	cmd.Flags().String("http-addr", "", "HTTP listen address")
	cmd.Flags().String("grpc-addr", "", "gRPC listen address")

	return cmd
}

func resolveServeConfig(flags *pflag.FlagSet) server.Config {
	cfg := server.DefaultConfig()
	if value := resolveStringValue(flags, "http-addr", "http_addr"); value != "" {
		cfg.HTTPAddr = value
	}
	if value := resolveStringValue(flags, "grpc-addr", "grpc_addr"); value != "" {
		cfg.GRPCAddr = value
	}

	return cfg
}

func resolveStringValue(flags *pflag.FlagSet, flagName, viperKey string) string {
	if flags.Changed(flagName) {
		value, err := flags.GetString(flagName)
		if err == nil {
			return value
		}
	}

	return viper.GetString(viperKey)
}
