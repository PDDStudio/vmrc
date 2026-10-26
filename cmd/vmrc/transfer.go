package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/pddstudio/vmrc/internal/atomicfile"
	"github.com/pddstudio/vmrc/internal/host"
	"github.com/urfave/cli/v3"
)

func getCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "download a file from an agent",
		ArgsUsage: "<target>:<remote> <local>   ('-' local = stdout)",
		Action:    getAction,
	}
}

func getAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 2 {
		return cli.Exit("usage: vmrc get <target>:<remote> <local>", 2)
	}
	target, remote, err := host.SplitTargetPath(cmd.Args().Get(0))
	if err != nil {
		return err
	}
	a, err := discoverer().Resolve(ctx, target, cmd.String("tag"))
	if err != nil {
		return err
	}
	rc, err := clientFor(cmd, a).Get(ctx, remote)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	local := cmd.Args().Get(1)
	if local == "-" {
		_, err = io.Copy(os.Stdout, rc)
		return err
	}

	n, err := atomicfile.Write(local, rc)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", n, local)
	return nil
}

func putCommand() *cli.Command {
	return &cli.Command{
		Name:      "put",
		Usage:     "upload a file into an agent's uploads dir",
		ArgsUsage: "<local> <target>:<remote>   ('-' local = stdin)",
		Description: "<remote> is relative to the agent's uploads dir (uploads_dir, default\n" +
			"<base_dir>/uploads), not its base dir. exec and spawn run in the base dir, so\n" +
			"they see the file at uploads/<remote>; fetch it back with\n" +
			"`vmrc get <target>:uploads/<remote> ...`. Uploads older than the agent's\n" +
			"upload_retention (default 7 days) are deleted automatically.",
		Action: putAction,
	}
}

func putAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 2 {
		return cli.Exit("usage: vmrc put <local> <target>:<remote>", 2)
	}
	local := cmd.Args().Get(0)
	target, remote, err := host.SplitTargetPath(cmd.Args().Get(1))
	if err != nil {
		return err
	}
	a, err := discoverer().Resolve(ctx, target, cmd.String("tag"))
	if err != nil {
		return err
	}

	var src io.Reader = os.Stdin
	if local != "-" {
		f, err := os.Open(local)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		src = f
	}
	if err := clientFor(cmd, a).Put(ctx, remote, src); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "uploaded to %s:%s\n", a.Name, remote)
	return nil
}
