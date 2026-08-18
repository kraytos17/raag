package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newDebugCmd exposes diagnostics for local debugging.
func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Diagnostics for local debugging",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "peers",
		Short: "Show peers with score, latency, and bandwidth",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.ListPeers()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			lp := resp.GetListPeers()
			if lp == nil {
				return nil
			}

			fmt.Fprintln(os.Stdout, "PEER\tCONN\tLATENCY\tBANDWIDTH\tSCORE")
			for _, p := range lp.Peers {
				conn := "no"
				if p.Connected {
					conn = "yes"
				}

				latency := "-"
				bandwidth := "-"
				score := "-"
				if sc := p.GetScore(); sc != nil {
					if sc.AvgLatencyMs > 0 {
						latency = fmt.Sprintf("%.0fms", sc.AvgLatencyMs)
					}
					if sc.AvgBandwidth > 0 {
						bandwidth = fmt.Sprintf("%.2fMB/s", float64(sc.AvgBandwidth)/1e6)
					}
					if sc.Score > 0 {
						score = fmt.Sprintf("%.1f", sc.Score)
					}
				}
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s\n", p.Id, conn, latency, bandwidth, score)
			}
			return nil
		},
	})
	return cmd
}
