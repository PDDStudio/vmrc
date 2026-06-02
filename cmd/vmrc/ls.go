package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"
)

func lsCommand() *cli.Command {
	return &cli.Command{
		Name:   "ls",
		Usage:  "list reachable vmrc agents on the tailnet",
		Action: lsAction,
	}
}

func lsAction(ctx context.Context, cmd *cli.Command) error {
	agents, err := discoverer().List(ctx, cmd.String("tag"))
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDNSNAME\tOS\tONLINE")
	for _, a := range agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%t\n", a.Name, a.DNSName, a.OS, a.Online)
	}
	return tw.Flush()
}
