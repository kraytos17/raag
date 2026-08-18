package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check the daemon's health",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.HealthCheck()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			hc := resp.GetHealthCheck()
			healthy := hc != nil && hc.Healthy
			fmt.Fprintf(os.Stdout, "healthy: %v\n", healthy)
			return nil
		},
	}
}
