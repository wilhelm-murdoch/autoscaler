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
)

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

// resolveAgentVMs returns every autoscaler-owned VM whose name matches the given
// agent name, searched cluster-wide. The autoscaler mints a unique name per
// agent, so in normal operation this resolves to at most one VM. Returning all
// matches keeps RemoveAgent able to garbage-collect strays that share a name
// (e.g. repeated simulator deploys with a fixed --name and --keep). The tag
// check guards against ever touching a VM the autoscaler did not create.
//
// VMs are enumerated per node via /nodes/{node}/qemu rather than via
// /cluster/resources: the latter is served from the pmxcfs status cache and lags
// roughly ten seconds, so a freshly cloned VM is invisible to it and removal
// would silently no-op right after a deploy.
func (p *provider) resolveAgentVMs(ctx context.Context, name string) ([]*px.VirtualMachine, error) {
	nodes, err := p.client.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	var vms []*px.VirtualMachine
	for _, nodeStatus := range nodes {
		node, err := p.client.Node(ctx, nodeStatus.Node)
		if err != nil {
			return nil, err
		}

		nodeVMs, err := node.VirtualMachines(ctx)
		if err != nil {
			return nil, err
		}

		for _, vm := range nodeVMs {
			if vm.Name == name && containsTag(vm.Tags) {
				vms = append(vms, vm)
			}
		}
	}

	return vms, nil
}

// Reference: https://pve.proxmox.com/pve-docs/qm.conf.5.html
const (
	deviceCountVirtIO = 16
	deviceCountSCSI   = 31
	deviceCountSATA   = 6
	deviceCountIDE    = 4
)

// resolveBootDisk returns the device name (e.g. "scsi0", "virtio0") of the
// clone's primary OS disk and skips cdrom/cloud-init drives. It is the fallback
// used when the template exposes no boot order of its own. Buses are scanned in
// PVE's own preference order and, within a bus, from the lowest index up, so the
// result is deterministic.
func resolveBootDisk(cfg *px.VirtualMachineConfig) (string, error) {
	groups := []struct {
		prefix  string
		count   int
		devices map[string]string
	}{
		{"virtio", deviceCountVirtIO, cfg.VirtIOs},
		{"scsi", deviceCountSCSI, cfg.SCSIs},
		{"sata", deviceCountSATA, cfg.SATAs},
		{"ide", deviceCountIDE, cfg.IDEs},
	}

	for _, group := range groups {
		for i := range group.count {
			name := fmt.Sprintf("%s%d", group.prefix, i)
			value, exists := group.devices[name]
			if !exists {
				continue
			}

			if strings.Contains(value, "media=cdrom") || strings.Contains(value, "cloudinit") {
				continue
			}

			wrapInfo("... found bootable disk %s", name)
			return name, nil
		}
	}

	return "", fmt.Errorf("%s: no bootable disk found on clone", Category)
}

func wrapError(message string, err error) error {
	return fmt.Errorf("%s: %s: %w", Category, message, err)
}

func wrapInfo(format string, values ...any) {
	log.Info().Msgf(fmt.Sprintf("%s: ", Category)+format, values...)
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
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s: guest agent did not respond within %s", Category, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			wrapInfo("... polling qemu")
		}
	}
}
