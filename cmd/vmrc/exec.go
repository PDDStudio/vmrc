package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/pddstudio/vmrc/internal/host"
	"github.com/urfave/cli/v3"
)

func execCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "run a command on one or more agents",
		ArgsUsage: "<targets> -- <cmd...>   (or --all -- <cmd...>)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "all", Usage: "fan out to every agent matching --tag"},
		},
		Action: execAction,
	}
}

func execAction(ctx context.Context, cmd *cli.Command) error {
	d := discoverer()
	tag := cmd.String("tag")
	args := cmd.Args().Slice()

	var agents []host.Agent
	var cmdline string

	if cmd.Bool("all") {
		if len(args) == 0 {
			return cli.Exit("usage: vmrc exec --all -- <cmd...>", 2)
		}
		all, err := d.List(ctx, tag)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			return cli.Exit(fmt.Sprintf("no agents matching %s", tag), 1)
		}
		agents = all
		cmdline = strings.Join(args, " ")
	} else {
		if len(args) < 2 {
			return cli.Exit("usage: vmrc exec <targets> -- <cmd...>", 2)
		}
		names, err := host.ParseTargets(args[0])
		if err != nil {
			return err
		}
		for _, n := range names {
			a, err := d.Resolve(ctx, n, tag)
			if err != nil {
				return err
			}
			agents = append(agents, a)
		}
		cmdline = strings.Join(args[1:], " ")
	}

	results := host.FanoutExec(ctx, agents, func(a host.Agent) *host.Client { return clientFor(cmd, a) }, cmdline)
	out, code := host.FormatExec(results, len(agents) > 1)
	fmt.Print(out)
	if code != 0 {
		return cli.Exit("", code)
	}
	return nil
}
