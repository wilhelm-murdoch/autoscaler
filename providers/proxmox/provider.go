package proxmox

import (
	"context"
	"fmt"
	"strings"
	"time"

	px "github.com/luthermonson/go-proxmox"
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

const agentTag = "woodpecker-autoscaler"

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
	fmt.Println(agent.Name)
	node, err := p.client.Node(ctx, p.node)
	if err != nil {
		return fmt.Errorf("could not get node: %w", err)
	}

	fmt.Println(node.Name)

	cluster, err := p.client.Cluster(ctx)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	fmt.Println(cluster.Name)

	newVMID, err := cluster.NextID(ctx)
	if err != nil {
		return fmt.Errorf("could not reserve VMID: %w", err)
	}

	fmt.Println(newVMID)

	return nil
}

func (p *provider) RemoveAgent(ctx context.Context, agent *woodpecker.Agent) error {
	return nil
}

func (p *provider) ListDeployedAgentNames(ctx context.Context) ([]string, error) {
	node, err := p.client.Node(ctx, p.node)
	if err != nil {
		return nil, fmt.Errorf("could not get node: %w", err)
	}

	containers, err := node.Containers(ctx)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, container := range containers {
		if containsTag(container.Tags) {
			names = append(names, container.Name)
		}
	}

	return names, nil
}

func containsTag(tags string) bool {
	return strings.Contains(";"+tags+";", ";"+agentTag+";")
}
