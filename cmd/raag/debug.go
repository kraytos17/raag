package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
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
		RunE:  runDebugPeers,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "streams",
		Short: "Show active stream pool stats",
		RunE:  runDebugStreams,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "index",
		Short: "Show search index stats",
		RunE:  runDebugIndex,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "db",
		Short: "Show database size",
		RunE:  runDebugDB,
	})
	return cmd
}

func runDebugPeers(cmd *cobra.Command, args []string) error {
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
}

// fetchDebugStats returns the daemon's aggregated debug stats, or an error.
func fetchDebugStats(c *ipc.Client) (*pb.DebugStatsResponse, error) {
	resp, err := c.DebugStats()
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, errors.New(resp.Error)
	}
	return resp.GetDebugStats(), nil
}

func runDebugStreams(cmd *cobra.Command, args []string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	ds, err := fetchDebugStats(client)
	if err != nil {
		return err
	}
	if ds == nil {
		return nil
	}
	if st := ds.GetStreams(); st != nil {
		fmt.Fprintf(os.Stdout, "total_streams: %d\npeer_count: %d\n", st.TotalStreams, st.PeerCount)
	} else {
		fmt.Fprintln(os.Stdout, "streams: not available (P2P disabled)")
	}
	return nil
}

func runDebugIndex(cmd *cobra.Command, args []string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	ds, err := fetchDebugStats(client)
	if err != nil {
		return err
	}
	if ds == nil {
		return nil
	}
	if ix := ds.GetIndex(); ix != nil {
		fmt.Fprintf(os.Stdout, "tracks: %d\nterms: %d\ntrigrams: %d\nlast_updated: %d\n",
			ix.TotalTracks, ix.TotalTerms, ix.TotalTrigrams, ix.LastUpdated)
	} else {
		fmt.Fprintln(os.Stdout, "index: not available")
	}
	return nil
}

func runDebugDB(cmd *cobra.Command, args []string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	ds, err := fetchDebugStats(client)
	if err != nil {
		return err
	}
	if ds == nil {
		return nil
	}
	if d := ds.GetDb(); d != nil {
		fmt.Fprintf(os.Stdout, "lsm_bytes: %d\nvlog_bytes: %d\n", d.LsmBytes, d.VlogBytes)
	} else {
		fmt.Fprintln(os.Stdout, "db: not available")
	}
	return nil
}
