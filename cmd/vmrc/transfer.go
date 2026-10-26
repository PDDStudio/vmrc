package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	n, err := downloadTo(local, rc)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", n, local)
	return nil
}

// downloadTo streams src into local atomically: it writes to a temp file in the
// same directory and renames it over local only after a fully successful copy
// and flush. A mid-transfer failure (dropped connection, disk-full on flush)
// therefore never truncates or leaves behind a partial local file — the
// pre-existing file, if any, is untouched. os.Create truncated the destination
// up front, destroying it on any failure.
func downloadTo(local string, src io.Reader) (int64, error) {
	dir := filepath.Dir(local)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(local)+".vmrc-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	n, err := io.Copy(tmp, src)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return n, err
	}
	// Flush/close before rename: a failed Close (flush/disk-full) means a
	// truncated temp file, so surface it and discard the temp rather than
	// promoting a corrupt file over the destination.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return n, fmt.Errorf("close %s: %w", tmpName, err)
	}
	// Fix the temp file's mode before promoting it: CreateTemp makes it 0600, but
	// replacing an existing file must keep that file's mode (e.g. an executable
	// stays +x) and a new file should follow os.Create semantics (0666 & ~umask).
	mode := os.FileMode(0o666) &^ processUmask()
	if fi, err := os.Stat(local); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return n, fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, local); err != nil {
		_ = os.Remove(tmpName)
		return n, fmt.Errorf("rename to %s: %w", local, err)
	}
	return n, nil
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
