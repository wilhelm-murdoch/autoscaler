package proxmox

import (
	"context"
	"fmt"
)

func (p *provider) ListDeployedAgentNames(ctx context.Context) ([]string, error) {
	node, err := p.client.Node(ctx, p.node)
	if err != nil {
		return nil, fmt.Errorf("could not get node: %w", err)
	}
	wrapInfo("searching node %s for agent vms", node.Name)

	vms, err := node.VirtualMachines(ctx)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, vm := range vms {
		if containsTag(vm.Tags) {
			wrapInfo("found agent vm %d:%s", vm.VMID, vm.Name)
			names = append(names, vm.Name)
		}
	}

	return names, nil
}
