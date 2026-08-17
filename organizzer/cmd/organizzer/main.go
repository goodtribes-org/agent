// Command organizzer drives one goodtribes-org project board.
//
// One binary, three workers, one per automated transition:
//
//	organizzer request   new    -> request   writes an outline
//	organizzer plan      plan   -> review    writes an implementation plan
//	organizzer apply     apply  -> test      dispatches the plan to an agent
//
// The two transitions in between — request → plan and review → apply — are
// approvals a human makes on the board. No worker can perform them; the board
// client refuses those targets outright.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/goodtribes-org/agent/organizzer/internal/config"
	"github.com/goodtribes-org/agent/organizzer/internal/service"
	"github.com/goodtribes-org/agent/organizzer/internal/stage"
)

// version is stamped at build time with -ldflags="-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return fmt.Errorf("no stage given")
	}

	name := os.Args[1]
	if name == "version" || name == "-version" || name == "--version" {
		fmt.Println("organizzer " + version)
		return nil
	}

	fs := flag.NewFlagSet("organizzer "+name, flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "read the board and print what would happen, changing nothing")
	once := fs.Bool("once", false, "run a single poll cycle and exit")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}

	var s stage.Stage
	switch name {
	case config.StageRequest:
		s = stage.Request{}
	case config.StagePlan:
		s = stage.Plan{}
	case config.StageApply:
		s = stage.Apply{}
	default:
		usage()
		return fmt.Errorf("unknown stage %q", name)
	}

	// Set before Load, not after: validation relaxes the credential rules for
	// a dry run, and it has already happened by the time Load returns.
	if *dryRun {
		if err := os.Setenv("DRY_RUN", "true"); err != nil {
			return err
		}
	}

	cfg, err := config.Load(name)
	if err != nil {
		return err
	}

	// The logger already carries the stage.
	log := service.Logger(name)
	log.Info("organizzer starting", "version", version,
		"org", cfg.GitHubOrg, "project", cfg.ProjectNumber, "model", cfg.Model)

	ctx, cancel := service.Context()
	defer cancel()

	// The health listener is skipped for a one-shot run: binding a port would
	// make two dry runs on the same machine collide for no benefit.
	var health *service.Health
	if !*once {
		health = service.NewHealth(cfg.HealthStaleFor)
		go health.Serve(ctx, cfg.HealthListen, log)
	}

	runner := stage.NewRunner(cfg, log)
	if err := stage.Run(ctx, runner, s, health, *once); err != nil {
		return err
	}
	log.Info("organizzer stopped")
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `organizzer — drives one goodtribes-org project board

usage: organizzer <stage> [flags]

stages:
  request   picks up cards in `+"`new`"+`, writes a request outline, moves them to `+"`request`"+`
  plan      picks up cards in `+"`plan`"+`, writes an implementation plan, moves them to `+"`review`"+`
  apply     picks up cards in `+"`apply`"+`, dispatches the plan to the implementation
            agent, moves them to `+"`test`"+`
  version   print the version and exit

flags:
  -dry-run  read the board and print what would happen, changing nothing
  -once     run a single poll cycle and exit

Configuration is entirely by environment variable; see the README.
`)
}
