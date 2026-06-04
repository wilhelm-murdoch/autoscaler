package proxmox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	px "github.com/luthermonson/go-proxmox"
	"github.com/urfave/cli/v3"

	"go.woodpecker-ci.org/autoscaler/config"
	"go.woodpecker-ci.org/autoscaler/engine/inits/cloudinit"
	"go.woodpecker-ci.org/autoscaler/engine/types"
	"go.woodpecker-ci.org/woodpecker/v3/woodpecker-go/woodpecker"
)

type provider struct {
	config       *config.Config
	client       *px.Client
	node         string
	templateVMID int
	storage      string
	bridge       string
	cores        int
	memory       int
}

const agentTag = "woodpecker-autoscaler"

func New(ctx context.Context, c *cli.Command, config *config.Config) (types.Provider, error) {
	opts := []px.Option{
		px.WithAPIToken(c.String("proxmox-token-id"), c.String("proxmox-token-secret")),
		px.WithTimeout(60 * time.Second),
	}

	if c.Bool("proxmox-insecure") {
		opts = append(opts, px.WithInsecureSkipVerify())
	}

	client := px.NewClient(c.String("proxmox-url"), opts...)

	p := &provider{
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
		return nil, fmt.Errorf("%s: node and template-vmid are required", Category)
	}

	return p, nil
}

func (p *provider) DeployAgent(ctx context.Context, agent *woodpecker.Agent) error {
	node, err := p.client.Node(ctx, p.node)
	if err != nil {
		return wrapError("could not get node", err)
	}

	cluster, err := p.client.Cluster(ctx)
	if err != nil {
		return wrapError("could not get cluster", err)
	}

	newVMID, err := cluster.NextID(ctx)
	if err != nil {
		return wrapError("could not reserve VMID", err)
	}

	templateNodeName, err := p.resolveVMTemplateNode(ctx, p.templateVMID)
	if err != nil {
		return wrapError("could not resolve template node name", err)
	}

	templateNode, err := p.client.Node(ctx, templateNodeName) // talk to B, the owner
	if err != nil {
		return wrapError("could not get template node", err)
	}

	template, err := templateNode.VirtualMachine(ctx, p.templateVMID)
	if err != nil {
		return wrapError(fmt.Sprintf("could not get template vm %d", p.templateVMID), err)
	}

	if !template.Template {
		return fmt.Errorf("%s: vm %d is not a template", Category, p.templateVMID)
	}
	fmt.Fprintf(os.Stderr, "DEBUG clone source node = %q, target = %q\n", template.Node, p.node)

	_, cloneTask, err := template.Clone(ctx, &px.VirtualMachineCloneOptions{
		NewID:   newVMID,
		Name:    agent.Name,
		Storage: p.storage,
		Full:    true,
		Target:  p.node,
	})
	if err != nil {
		return wrapError("could not clone", err)
	}

	if err := waitFor(ctx, cloneTask); err != nil {
		return wrapError("clone task failed", err)
	}

	vm, err := node.VirtualMachine(ctx, newVMID)
	if err != nil {
		return fmt.Errorf("get new vm: %w", err)
	}

	userData, err := cloudinit.RenderUserDataTemplate(p.config, agent, cloudinit.RenderOption{})
	if err != nil {
		return wrapError("cloudinit.RenderUserDataTemplate", err)
	}

	fmt.Println(userData)
	fmt.Println(vm.Status)

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

	vms, err := node.VirtualMachines(ctx)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, vm := range vms {
		if containsTag(vm.Tags) {
			names = append(names, vm.Name)
		}
	}

	return names, nil
}

// The desired template may, or may not, exist on the target cluster node. This
// method allows us to search all cluster nodes for the desired template and
// return the associated node.
func (p *provider) resolveVMTemplateNode(ctx context.Context, vmid int) (string, error) {
	cluster, err := p.client.Cluster(ctx)
	if err != nil {
		return "", err
	}

	resources, err := cluster.Resources(ctx, "vm") // type=vm filter
	if err != nil {
		return "", err
	}

	for _, resource := range resources {
		if int(resource.VMID) == vmid {
			return resource.Node, nil
		}
	}
	return "", fmt.Errorf("%s: vm %d not found anywhere in cluster", Category, vmid)
}

func containsTag(tags string) bool {
	return strings.Contains(";"+tags+";", ";"+agentTag+";")
}

func wrapError(message string, err error) error {
	return fmt.Errorf("%s: %s: %w", Category, message, err)
}

func waitFor(ctx context.Context, t *px.Task) error {
	if t == nil {
		return nil
	}

	return t.Wait(ctx, 1*time.Second, 5*time.Minute)
}
