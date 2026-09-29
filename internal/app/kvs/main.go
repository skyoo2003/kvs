// Package app implements the KVS command-line application.
package app

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Execute runs the CLI with the supplied program arguments and build version.
func Execute(args []string, stdout, stderr io.Writer, version string) error {
	cmd := newRootCmd(stdout, stderr, version)
	cmd.SetArgs(args)

	return cmd.Execute()
}

func newRootCmd(stdout, stderr io.Writer, version string) *cobra.Command {
	var cfgFile string
	var showVersion bool

	rootCmd := &cobra.Command{
		Use:           "kvs",
		Short:         "A simple key-value store CLI",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return initConfig(cfgFile)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
				return err
			}

			return cmd.Help()
		},
	}

	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "print version")
	rootCmd.AddCommand(newVersionCmd(version))
	rootCmd.AddCommand(newServeCmd())

	return rootCmd
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print the CLI version",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}

func initConfig(cfgFile string) error {
	viper.Reset()
	viper.SetEnvPrefix("KVS")
	viper.AutomaticEnv()

	if cfgFile == "" {
		return nil
	}

	viper.SetConfigFile(cfgFile)

	return viper.ReadInConfig()
}
