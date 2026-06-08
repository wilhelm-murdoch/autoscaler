package proxmox

import (
	"context"
	"fmt"
	"net/http"
	"time"

	px "github.com/luthermonson/go-proxmox"
	"github.com/urfave/cli/v3"

	"go.woodpecker-ci.org/autoscaler/config"
	"go.woodpecker-ci.org/autoscaler/engine/types"
)

type provider struct {
	config        *config.Config
	client        *px.Client
	node          string
	fullClone     bool
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

	// Intercept HTTP requests and return their responses from the PVE cluster API
	if c.Bool("proxmox-debug") {
		httpRoundTripperClient := &http.Client{Transport: httpDebugger{http.DefaultTransport}}
		opts = append(opts, px.WithHTTPClient(httpRoundTripperClient))
	}

	client := px.NewClient(c.String("proxmox-url"), opts...)

	p := &provider{
		config:        config,
		client:        client,
		node:          c.String("proxmox-node"),
		fullClone:     c.Bool("proxmox-full-clone"),
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
