package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func shotCommand() *cli.Command {
	return &cli.Command{
		Name:      "shot",
		Usage:     "capture a screenshot from one agent",
		ArgsUsage: "<target>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "output PNG path (default <name>.png, '-' = stdout)"},
		},
		Action: shotAction,
	}
}

func shotAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cli.Exit("usage: vmrc shot <target> [-o file.png]", 2)
	}
	a, err := discoverer().Resolve(ctx, cmd.Args().First(), cmd.String("tag"))
	if err != nil {
		return err
	}
	png, err := clientFor(cmd, a).Screenshot(ctx)
	if err != nil {
		return err
	}
	out := cmd.String("out")
	if out == "" {
		out = a.Name + ".png"
	}
	if out == "-" {
		_, err := os.Stdout.Write(png)
		return err
	}
	if err := os.WriteFile(out, png, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", len(png), out)
	return nil
}
