package proxmox

import "github.com/urfave/cli/v3"

const Category = "proxmox"

var ProviderFlags = []cli.Flag{
	// proxmox
	&cli.StringFlag{
		Name:     "proxmox-url",
		Usage:    "Proxmox API URL, e.g. https://pve.example.tld:8006/api2/json",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_URL"),
		Category: Category,
	},
	&cli.StringFlag{
		Name:     "proxmox-token-id",
		Usage:    "API token ID, e.g. autoscaler@pve!agents",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_TOKEN_ID"),
		Category: Category,
	},
	&cli.StringFlag{
		Name:     "proxmox-token-secret",
		Usage:    "API token secret",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_TOKEN_SECRET"),
		Category: Category,
	},
	&cli.StringFlag{
		Name:     "proxmox-node",
		Usage:    "target Proxmox node name",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_NODE"),
		Category: Category,
	},
	&cli.IntFlag{
		Name:     "proxmox-template-vmid",
		Usage:    "VMID of the LXC template to clone (must be a template; baked with docker + woodpecker-agent as a oneshot unit)",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_TEMPLATE_VMID"),
		Category: Category,
	},
	&cli.StringFlag{
		Name:     "proxmox-storage",
		Value:    "local-zfs",
		Usage:    "storage for the cloned rootfs",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_STORAGE"),
		Category: Category,
	},
	&cli.StringFlag{
		Name:     "proxmox-bridge",
		Value:    "vmbr0",
		Usage:    "network bridge for net0",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_BRIDGE"),
		Category: Category,
	},
	&cli.IntFlag{
		Name:     "proxmox-cores",
		Value:    2,
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_CORES"),
		Category: Category,
	},
	&cli.IntFlag{
		Name:     "proxmox-memory",
		Value:    2048,
		Usage:    "memory in MiB",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_MEMORY"),
		Category: Category,
	},
	&cli.BoolFlag{
		Name:     "proxmox-insecure",
		Usage:    "skip TLS verification (lab only)",
		Sources:  cli.EnvVars("WOODPECKER_PROXMOX_INSECURE"),
		Category: Category,
	},
}
