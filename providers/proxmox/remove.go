package proxmox

import (
	"context"
	"fmt"

	"go.woodpecker-ci.org/woodpecker/v3/woodpecker-go/woodpecker"
)

func (p *provider) RemoveAgent(ctx context.Context, agent *woodpecker.Agent) error {
	vms, err := p.resolveAgentVMs(ctx, agent.Name)
	if err != nil {
		return wrapError("could not resolve any matching agent vms", err)
	}
	wrapInfo("found %d matching agent vms", len(vms))

	for _, vm := range vms {
		if vm.IsRunning() {
			wrapInfo("stopping agent %d:%s", vm.VMID, vm.Name)
			stopTask, err := vm.Stop(ctx)
			if err != nil {
				return wrapError(fmt.Sprintf("could not stop agent vm %d:%s", vm.VMID, vm.Name), err)
			}

			if err := waitFor(ctx, stopTask); err != nil {
				return wrapError(fmt.Sprintf("stop task failed for vm %d:%s", vm.VMID, vm.Name), err)
			}
		}

		wrapInfo("deleting agent %d:%s", vm.VMID, vm.Name)
		deleteTask, err := vm.Delete(ctx)
		if err != nil {
			return wrapError(fmt.Sprintf("could not delete agent vm %d", vm.VMID), err)
		}

		if err := waitFor(ctx, deleteTask); err != nil {
			return wrapError(fmt.Sprintf("delete task failed for vm %d", vm.VMID), err)
		}
	}

	return nil
}
