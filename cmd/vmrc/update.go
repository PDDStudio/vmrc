package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/pddstudio/vmrc/internal/host"
	"github.com/pddstudio/vmrc/internal/selfupdate"
	"github.com/urfave/cli/v3"
)

// updateRepo is the GitHub repo releases are pulled from. Inline here (rather
// than a shared package) to keep the two main packages independent.
const updateRepo = "pddstudio/vmrc"

func updateCommand() *cli.Command {
	return &cli.Command{
		Name:      "update",
		Usage:     "self-update this CLI (--self) or update agents (--all / <target>...)",
		ArgsUsage: "[<target>...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "self", Usage: "update this local vmrc binary"},
			&cli.BoolFlag{Name: "all", Usage: "update every reachable agent matching --tag"},
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
	self := cmd.Bool("self")
	all := cmd.Bool("all")
	targets := cmd.Args().Slice()
	remote := all || len(targets) > 0

	switch {
	case self && remote:
		return cli.Exit("choose either --self or agent targets, not both", 2)
	case !self && !remote:
		return cli.Exit("specify --self (update this CLI), --all, or one or more agent targets", 2)
	case self:
		return selfUpdate(ctx, cmd)
	default:
		return remoteUpdate(ctx, cmd, all, targets)
	}
}

// updateParams reads the shared self-update flags into a selfupdate.Options-ish
// bundle reused for both the local and remote paths.
func remoteParams(cmd *cli.Command) host.UpdateParams {
	return host.UpdateParams{
		Version: cmd.String("version"),
		Pre:     cmd.Bool("pre"),
		Force:   cmd.Bool("force"),
	}
}

// selfUpdate updates the running vmrc binary.
func selfUpdate(ctx context.Context, cmd *cli.Command) error {
	t := selfupdate.Target{
		Repo:       updateRepo,
		BinaryName: "vmrc",
		Version:    version,
		Token:      cmd.String("github-token"),
	}
	rel, err := selfupdate.Check(ctx, t, selfupdate.Options{
		PinnedTag:       cmd.String("version"),
		AllowPrerelease: cmd.Bool("pre"),
	})
	if errors.Is(err, selfupdate.ErrUpToDate) {
		fmt.Printf("vmrc is up to date (%s)\n", version)
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
	if err := selfupdate.Apply(ctx, t, rel, exe); err != nil {
		return err
	}
	fmt.Printf("updated vmrc to %s at %s; re-run to use it\n", rel.TagName, exe)
	return nil
}

// remoteUpdate drives one or more agents to self-update over the tailnet.
func remoteUpdate(ctx context.Context, cmd *cli.Command, all bool, targets []string) error {
	agents, err := resolveAgents(ctx, cmd, all, targets)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return cli.Exit("no agents to update", 1)
	}
	check := cmd.Bool("check")
	params := remoteParams(cmd)

	type outcome struct {
		name string
		line string
		err  error
	}
	results := make([]outcome, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a host.Agent) {
			defer wg.Done()
			c := clientFor(cmd, a)
			if check {
				st, err := c.CheckUpdate(ctx, params)
				if err != nil {
					results[i] = outcome{a.Name, "", err}
					return
				}
				line := fmt.Sprintf("up to date (%s)", st.Current)
				if st.Available {
					line = fmt.Sprintf("update available: %s -> %s", st.Current, st.Latest)
				}
				results[i] = outcome{a.Name, line, nil}
				return
			}
			res, err := c.Update(ctx, params)
			if err != nil {
				results[i] = outcome{a.Name, "", err}
				return
			}
			line := "restarting with new binary"
			if res.Old != "" {
				line = fmt.Sprintf("%s -> %s (restarting)", res.Old, res.New)
			}
			results[i] = outcome{a.Name, line, nil}
		}(i, a)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })

	code := 0
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", r.name, r.err)
			code = 1
			continue
		}
		fmt.Printf("%s: %s\n", r.name, r.line)
	}
	if code != 0 {
		return cli.Exit("", code)
	}
	return nil
}

// resolveAgents collects the target agents, either every tagged agent (--all) or
// the named/comma-separated positional targets.
func resolveAgents(ctx context.Context, cmd *cli.Command, all bool, targets []string) ([]host.Agent, error) {
	d := discoverer()
	tag := cmd.String("tag")
	if all {
		agents, err := d.List(ctx, tag)
		if err != nil {
			return nil, err
		}
		if len(agents) == 0 {
			return nil, cli.Exit(fmt.Sprintf("no agents matching %s", tag), 1)
		}
		return agents, nil
	}
	var agents []host.Agent
	for _, arg := range targets {
		names, err := host.ParseTargets(arg)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			a, err := d.Resolve(ctx, n, tag)
			if err != nil {
				return nil, err
			}
			agents = append(agents, a)
		}
	}
	return agents, nil
}
