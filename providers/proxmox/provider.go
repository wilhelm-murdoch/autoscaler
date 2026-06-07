package proxmox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	px "github.com/luthermonson/go-proxmox"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"go.woodpecker-ci.org/autoscaler/config"
	"go.woodpecker-ci.org/autoscaler/engine/inits/cloudinit"
	"go.woodpecker-ci.org/autoscaler/engine/types"
	"go.woodpecker-ci.org/woodpecker/v3/woodpecker-go/woodpecker"
)

type provider struct {
	config        *config.Config
	client        *px.Client
	node          string
	templateVMID  int
	storageRootFS string
	storageISO    string
	bridge        string
	cores         int
	memory        int
}

const (
	agentTag         = "woodpecker-autoscaler"
	agentDescription = "Provisioned by the Woodpecker CI Autoscaler Proxmox provider."
)

func New(ctx context.Context, c *cli.Command, config *config.Config) (types.Provider, error) {
	opts := []px.Option{
		px.WithAPIToken(c.String("proxmox-token-id"), c.String("proxmox-token-secret")),
		px.WithTimeout(60 * time.Second),
	}

	if c.Bool("proxmox-insecure") {
		opts = append(opts, px.WithInsecureSkipVerify())
	}

	if c.Bool("proxmox-debug") {
		hc := &http.Client{Transport: httpDebugger{http.DefaultTransport}}
		opts = append(opts, px.WithHTTPClient(hc))
	}

	client := px.NewClient(c.String("proxmox-url"), opts...)

	config.Image = "woodpeckerci/woodpecker-agent:latest"

	p := &provider{
		config:        config,
		client:        client,
		node:          c.String("proxmox-node"),
		templateVMID:  c.Int("proxmox-template-vmid"),
		storageRootFS: c.String("proxmox-storage-rootfs"),
		storageISO:    c.String("proxmox-storage-iso"),
		bridge:        c.String("proxmox-bridge"),
		cores:         c.Int("proxmox-cores"),
		memory:        c.Int("proxmox-memory"),
	}

	if p.node == "" || p.templateVMID == 0 {
		return nil, fmt.Errorf("%s: node and template-vmid are required", Category)
	}

	return p, nil
}

// TODO: rename node to nodeTarget and templateNode(Name) to nodeTemplate

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

	_, cloneTask, err := template.Clone(ctx, &px.VirtualMachineCloneOptions{
		NewID:   newVMID,
		Name:    agent.Name,
		Storage: p.storageRootFS,
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
		return wrapError("could not get new agent vm", err)
	}

	if err := vm.ConfigSync(ctx, px.VirtualMachineOption{Name: "description", Value: agentDescription}); err != nil {
		return wrapError("could not set agent vm options", err)
	}

	userData, err := cloudinit.RenderUserDataTemplate(p.config, agent, cloudinit.RenderOption{})
	if err != nil {
		return wrapError("could not render user data", err)
	}

	// CloudInit builds the ISO, uploads it to storage, attaches it to the given
	// device and sets boot to "<current boot>;<device>". The clone's cached Boot
	// is empty, so seed a valid order here to avoid PVE rejecting ";ide2".
	vm.VirtualMachineConfig.Boot = "order=virtio0"

	if err := vm.CloudInit(ctx, "ide2", userData, "", "", "", px.WithCloudInitStorage(p.storageISO)); err != nil {
		return wrapError("could not attach user data to vm", err)
	}

	startTask, err := vm.Start(ctx)
	if err != nil {
		return wrapError("could not start agent vm", err)
	}

	if err := waitFor(ctx, startTask); err != nil {
		return wrapError("agent vm timed out", err)
	}

	if err := waitForQemuAgent(ctx, vm, 180*time.Second); err != nil {
		return wrapError("qemu agent timed out", err)
	}

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
// return the associated node. Note that in order to use a template located on
// a seperate node it must be stored on shared storage accessable to the target
// node as well.
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

type httpDebugger struct{ rt http.RoundTripper }

func (d httpDebugger) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(os.Stderr, ">> %s %s\n   body: %s\n", r.Method, r.URL, debugBody(b))
		r.Body = io.NopCloser(bytes.NewReader(b))
	}
	return d.rt.RoundTrip(r)
}

// debugBody returns the body as a string, or a placeholder when it contains
// binary data that would corrupt terminal output.
func debugBody(b []byte) string {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) != -1 {
		return "<skipping binary output>"
	}
	return string(b)
}

// waitForQemuAgent polls the guest agent until it responds or the timeout
// elapses. Unlike px.VirtualMachine.WaitForAgent, which only retries on the
// exact "500 QEMU guest agent is not running" message and returns immediately
// on any other error, this treats every error as "not ready yet" so transient
// boot-time responses don't abort the wait early.
func waitForQemuAgent(ctx context.Context, vm *px.VirtualMachine, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		if _, err := vm.AgentOsInfo(ctx); err == nil {
			log.Info().Msgf("qemu agent up")
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s: guest agent did not respond within %s", Category, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			log.Info().Msgf("waiting for qemu agent")
		}
	}
}
