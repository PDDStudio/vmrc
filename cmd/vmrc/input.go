package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"
)

func clickCommand() *cli.Command {
	return &cli.Command{
		Name:      "click",
		Usage:     "click the mouse at screen coordinates on one agent",
		ArgsUsage: "<target> <x> <y>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "button", Aliases: []string{"b"}, Value: "left", Usage: "mouse button: left, right, or middle"},
			&cli.BoolFlag{Name: "double", Aliases: []string{"d"}, Usage: "double-click"},
		},
		Action: clickAction,
	}
}

func clickAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 3 {
		return cli.Exit("usage: vmrc click <target> <x> <y> [--button left|right|middle] [--double]", 2)
	}
	x, err := strconv.Atoi(cmd.Args().Get(1))
	if err != nil {
		return cli.Exit("x must be an integer", 2)
	}
	y, err := strconv.Atoi(cmd.Args().Get(2))
	if err != nil {
		return cli.Exit("y must be an integer", 2)
	}
	a, err := discoverer().Resolve(ctx, cmd.Args().First(), cmd.String("tag"))
	if err != nil {
		return err
	}
	if err := clientFor(cmd, a).Click(ctx, x, y, cmd.String("button"), cmd.Bool("double")); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "clicked %s at (%d,%d)\n", a.Name, x, y)
	return nil
}

func typeCommand() *cli.Command {
	return &cli.Command{
		Name:      "type",
		Usage:     "type literal text on one agent",
		ArgsUsage: "<target> <text>",
		Action:    typeAction,
	}
}

func typeAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 2 {
		return cli.Exit("usage: vmrc type <target> <text>", 2)
	}
	a, err := discoverer().Resolve(ctx, cmd.Args().First(), cmd.String("tag"))
	if err != nil {
		return err
	}
	text := cmd.Args().Get(1)
	if err := clientFor(cmd, a).Type(ctx, text); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "typed %d chars on %s\n", len([]rune(text)), a.Name)
	return nil
}

func keyCommand() *cli.Command {
	return &cli.Command{
		Name:      "key",
		Usage:     "press a key combo on one agent (e.g. Enter, Ctrl+C)",
		ArgsUsage: "<target> <combo>",
		Action:    keyAction,
	}
}

func keyAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 2 {
		return cli.Exit(`usage: vmrc key <target> <combo>   (e.g. "Enter", "Ctrl+C", "Alt+F4")`, 2)
	}
	a, err := discoverer().Resolve(ctx, cmd.Args().First(), cmd.String("tag"))
	if err != nil {
		return err
	}
	combo := cmd.Args().Get(1)
	if err := clientFor(cmd, a).Key(ctx, combo); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "pressed %s on %s\n", combo, a.Name)
	return nil
}
