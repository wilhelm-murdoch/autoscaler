package proxmox

import (
	"context"
	"fmt"
	"time"

	px "github.com/luthermonson/go-proxmox"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"go.woodpecker-ci.org/autoscaler/config"
	"go.woodpecker-ci.org/autoscaler/engine/types"
	"go.woodpecker-ci.org/woodpecker/v3/woodpecker-go/woodpecker"
)

type provider struct {
	config *config.Config
	client *px.Client

	node         string
	templateVMID int
	storage      string
	bridge       string
	cores        int
	memory       int
}

const (
	agentTag        = "woodpecker-autoscaler"
	agentNamePrefix = "woodpecker-agent"
)

func New(ctx context.Context, c *cli.Command, config *config.Config) (types.Provider, error) {
	p := &provider{}

	opts := []px.Option{
		px.WithAPIToken(c.String("proxmox-token-id"), c.String("proxmox-token-secret")),
		px.WithTimeout(60 * time.Second),
	}

	if c.Bool("proxmox-insecure") {
		opts = append(opts, px.WithInsecureSkipVerify())
	}

	client := px.NewClient(c.String("proxmox-url"), opts...)

	p = &provider{
		config:       config,
		client:       client,
		node:         c.String("proxmox-node"),
		templateVMID: c.Int("proxmox-template-vmid"),
		storage:      c.String("proxmox-storage"),
		bridge:       c.String("proxmox-bridge"),
		cores:        c.Int("proxmox-cores"),
		memory:       c.Int("proxmox-memory"),
	}

	if p.node == "" || p.templateVMID == 0 {
		return nil, fmt.Errorf("proxmox: node and template-vmid are required")
	}

	return p, nil
}

func (p *provider) DeployAgent(ctx context.Context, agent *woodpecker.Agent) error {
	return nil
}

func (p *provider) RemoveAgent(ctx context.Context, agent *woodpecker.Agent) error {
	return nil
}

func (p *provider) ListDeployedAgentNames(ctx context.Context) ([]string, error) {
	log.Debug().Msgf("list deployed agent names")

	var names []string

	return names, nil
}
