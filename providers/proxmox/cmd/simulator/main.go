// Command `simulator` drives the Proxmox autoscaler provider directly against a
// live PVE cluster. This means no woodpecker-server or simulated builds.
//
// The provider only needs a `*woodpecker.Agent` with a name and token, so we
// fabricate one. A fake token won't let the agent register with a real server,
// but the provider and agent lifecycle is fully exercised.
//
//	go run ./providers/proxmox/simulator/main.go deploy \
//		--name smoke-test-1 \
//		--keep \
//		--proxmox-url https://pve.domain.tld:8006/api2/json \
//		--proxmox-token-id autoscaler@pve!agents \
//		--proxmox-token-secret xxxx \
//		--proxmox-node pve1 \
//		--proxmox-template-vmid 9000 \
//		--proxmox-insecure
//	go run ./providers/proxmox/simulator/main.go list ... same flags as `deploy` ...
//	go run ./providers/proxmox/simulator/main.go remove --name smoke-test-1  ... same flags as `deploy` ...

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
	"go.woodpecker-ci.org/autoscaler/config"
	proxmox "go.woodpecker-ci.org/autoscaler/providers/proxmox"
	"go.woodpecker-ci.org/woodpecker/v3/woodpecker-go/woodpecker"
)

func main() {
	app := &cli.Command{
		Name:  "proxmox-simulator",
		Usage: "exercise the Proxmox autoscaler provider against a live PVE cluster",
		Flags: proxmox.ProviderFlags,
		Commands: []*cli.Command{
			{
				Name:  "deploy",
				Usage: "clone, configure, start and provision one agent container",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "name",
						Value: "woodpecker-agent-simulated",
						Usage: "the test agent name",
					},
					&cli.StringFlag{
						Name:  "token",
						Value: "beep-boop",
						Usage: "a fake per-agent token",
					},
					&cli.BoolFlag{
						Name:  "keep",
						Usage: "do not garbage collect; useful for debugging",
					},
				},
				Action: deploy,
			},
			{
				Name:   "list",
				Usage:  "list containers this autoscaler owns",
				Action: list,
			},
			{
				Name:  "remove",
				Usage: "stop and delete the container for an agent name",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Required: true},
				},
				Action: remove,
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// buildProvider wires the real `New()` exactly as the autoscaler would.
func buildProvider(ctx context.Context, cmd *cli.Command) (engineProvider, error) {
	cfg := &config.Config{
		GRPCAddress: cmd.String("grpc-addr"),
		GRPCSecure:  cmd.Bool("grpc-secure"),
	}

	return proxmox.New(ctx, cmd, cfg)
}

// engineProvider is the subset of `engine.Provider` we call here.
type engineProvider interface {
	DeployAgent(ctx context.Context, agent *woodpecker.Agent) error
	ListDeployedAgentNames(ctx context.Context) ([]string, error)
	RemoveAgent(ctx context.Context, agent *woodpecker.Agent) error
}

func deploy(ctx context.Context, cmd *cli.Command) error {
	provider, err := buildProvider(ctx, cmd)
	if err != nil {
		return err
	}

	agent := &woodpecker.Agent{
		Name:  cmd.String("name"),
		Token: cmd.String("token"),
	}

	log.Info().Msgf("deploying %q...\n", agent.Name)
	if err := provider.DeployAgent(ctx, agent); err != nil {
		return fmt.Errorf("deploy failed: %w", err)
	}
	log.Info().Msg("agent deployed OK")

	if cmd.Bool("keep") {
		log.Info().Msg("--keep flag specified; exiting without tearing down")
		return nil
	}

	log.Info().Msg("tearing agent down...")
	return provider.RemoveAgent(ctx, agent)
}

func list(ctx context.Context, cmd *cli.Command) error {
	provider, err := buildProvider(ctx, cmd)
	if err != nil {
		return err
	}

	names, err := provider.ListDeployedAgentNames(context.Background())
	log.Info().Msgf("searching for deployed agents")
	if err != nil {
		return err
	}

	if len(names) == 0 {
		log.Info().Msgf("no agents found")
		return nil
	}

	for _, name := range names {
		log.Info().Msgf("agent found: %s", name)
	}

	return nil
}

func remove(ctx context.Context, cmd *cli.Command) error {
	provider, err := buildProvider(ctx, cmd)
	if err != nil {
		return err
	}

	return provider.RemoveAgent(
		context.Background(),
		&woodpecker.Agent{Name: cmd.String("name")},
	)
}
