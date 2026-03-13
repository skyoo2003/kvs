// Package main implements to execute the program.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version = "dev"

func main() {
	if err := execute(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(args []string, stdout io.Writer, stderr io.Writer) error {
	cmd := newRootCmd(stdout, stderr)
	cmd.SetArgs(args)

	return cmd.Execute()
}

func newRootCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
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
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

func newVersionCmd() *cobra.Command {
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
