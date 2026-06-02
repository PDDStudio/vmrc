package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/pddstudio/vmrc/internal/selfupdate"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/urfave/cli/v3"
)

// updateRepo is the GitHub repo releases are pulled from. Inline here (rather
// than a shared package) to keep the two main packages independent.
const updateRepo = "pddstudio/vmrc"

func updateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "self-update the agent binary (stops, replaces, then restarts the service)",
		Flags: []cli.Flag{
			configFlag(),
			serviceNameFlag(),
			&cli.BoolFlag{Name: "check", Usage: "report the available version without installing"},
			&cli.StringFlag{Name: "version", Usage: "install a specific release tag (allows reinstall/downgrade)"},
			&cli.BoolFlag{Name: "force", Usage: "allow updating a dev build"},
			&cli.BoolFlag{Name: "pre", Usage: "consider prereleases"},
			&cli.StringFlag{Name: "github-token", Usage: "optional GitHub token (raises the API rate limit)", Sources: cli.EnvVars("GITHUB_TOKEN")},
		},
		Action: updateAction,
	}
}

func updateAction(ctx context.Context, cmd *cli.Command) error {
	t := selfupdate.Target{
		Repo:       updateRepo,
		BinaryName: serviceName, // "vmrc-agent" — also the goreleaser binary id
		Version:    version,
		Token:      cmd.String("github-token"),
	}
	rel, err := selfupdate.Check(ctx, t, selfupdate.Options{
		PinnedTag:       cmd.String("version"),
		AllowPrerelease: cmd.Bool("pre"),
	})
	if errors.Is(err, selfupdate.ErrUpToDate) {
		fmt.Printf("vmrc-agent is up to date (%s)\n", version)
		return nil
	}
	if err != nil {
		return err
	}
	if err := selfupdate.GateDevBuild(version, cmd.Bool("force"), cmd.String("version")); err != nil {
		return cli.Exit(err.Error(), 1)
	}
	label := rel.TagName
	if rel.Prerelease {
		label += " (prerelease)"
	}
	if cmd.Bool("check") {
		fmt.Printf("update available: %s -> %s\n", version, label)
		if rel.Notes != "" {
			fmt.Printf("\n%s\n", rel.Notes)
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving own path: %w", err)
	}

	mgr, err := buildManager(cmd)
	if err != nil {
		return err
	}
	// Status errors are tolerated: the service may not be installed, in which case
	// we simply replace the binary and let the operator start it.
	st, _ := mgr.Status()
	stopped := false
	if st == "running" {
		if err := service.RequireElevated(); err != nil {
			return err
		}
		fmt.Printf("stopping service %q...\n", cmd.String("service-name"))
		if err := mgr.Control("stop"); err != nil {
			return fmt.Errorf("stopping service before update: %w", err)
		}
		stopped = true
	}

	fmt.Printf("replacing %s ...\n", exe)
	if err := selfupdate.Apply(ctx, t, rel, exe); err != nil {
		// Rollback-restart: if we stopped the service, bring the OLD binary back up
		// so the agent is never left down on a failed update.
		if stopped {
			if serr := mgr.Control("start"); serr != nil {
				return fmt.Errorf("update failed (%v) and restarting the old binary also failed: %w", err, serr)
			}
			return fmt.Errorf("update failed; restarted the previous binary: %w", err)
		}
		return err
	}

	if stopped {
		fmt.Printf("starting service %q with the new binary...\n", cmd.String("service-name"))
		if err := mgr.Control("start"); err != nil {
			return fmt.Errorf("binary replaced but starting the service failed (start it manually): %w", err)
		}
		newSt, _ := mgr.Status()
		fmt.Printf("updated vmrc-agent to %s at %s; service %q: %s\n", rel.TagName, exe, cmd.String("service-name"), newSt)
		return nil
	}

	fmt.Printf("updated vmrc-agent to %s at %s; (re)start the service to run it\n", rel.TagName, exe)
	return nil
}
