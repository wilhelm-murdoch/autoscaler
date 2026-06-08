package proxmox

import (
	"context"
	"fmt"
	"strings"
	"time"

	px "github.com/luthermonson/go-proxmox"
	"go.woodpecker-ci.org/autoscaler/engine/inits/cloudinit"
	"go.woodpecker-ci.org/woodpecker/v3/woodpecker-go/woodpecker"
)

func (p *provider) DeployAgent(ctx context.Context, agent *woodpecker.Agent) error {
	wrapInfo("searching for target cluster node %s", p.node)
	nodeTarget, err := p.client.Node(ctx, p.node)
	if err != nil {
		return wrapError("could not get node", err)
	}
	wrapInfo("... found cluster node %s running PVE %s", nodeTarget.Name, nodeTarget.PVEVersion)

	wrapInfo("searching for %s's assigned cluster", nodeTarget.Name)
	cluster, err := p.client.Cluster(ctx)
	if err != nil {
		return wrapError("could not get cluster", err)
	}
	wrapInfo("... found cluster %s", cluster.Name)

	wrapInfo("reserving new VMID")
	newVMID, err := cluster.NextID(ctx)
	if err != nil {
		return wrapError("could not reserve VMID", err)
	}
	wrapInfo("... VMID %d reserved", newVMID)

	wrapInfo("searching %s cluster for specified template's %d assigned node", cluster.Name, p.templateVMID)
	nodeTemplateName, err := p.resolveVMTemplateNode(ctx, p.templateVMID)
	if err != nil {
		return wrapError("could not resolve template node name", err)
	}

	nodeTemplate, err := p.client.Node(ctx, nodeTemplateName)
	if err != nil {
		return wrapError("could not get template node details", err)
	}
	wrapInfo("... found template %d assigned to %s cluster node %s", p.templateVMID, cluster.Name, nodeTemplateName)

	wrapInfo("searching %s cluster for specified template %d vm", cluster.Name, p.templateVMID)
	templateVM, err := nodeTemplate.VirtualMachine(ctx, p.templateVMID)
	if err != nil {
		return wrapError(fmt.Sprintf("could not get template vm %d", p.templateVMID), err)
	}

	if !templateVM.Template {
		return fmt.Errorf("%s: vm %d is not a template", Category, p.templateVMID)
	}
	wrapInfo("... found template vm %d:%s", templateVM.VMID, templateVM.Name)

	VMCloneOptions := &px.VirtualMachineCloneOptions{
		NewID: newVMID,
		Name:  agent.Name,
	}

	if p.fullClone {
		wrapInfo("configuring new agent vm as a full clone")
		VMCloneOptions.Storage = p.storageRootFS
		VMCloneOptions.Full = true
		VMCloneOptions.Target = p.node
	} else {
		wrapInfo("configuring new agent vm as a linked clone")
		VMCloneOptions.Target = nodeTemplateName
	}

	wrapInfo("cloning template %d:%s from node %s to node %s", templateVM.VMID, templateVM.Name, nodeTemplate.Name, nodeTarget.Name)
	_, cloneTask, err := templateVM.Clone(ctx, VMCloneOptions)
	if err != nil {
		return wrapError("could not clone template", err)
	}

	if err := waitFor(ctx, cloneTask); err != nil {
		return wrapError("template cloning timed out", err)
	}

	vm, err := nodeTarget.VirtualMachine(ctx, newVMID)
	if err != nil {
		return wrapError("could not get new agent vm", err)
	}
	wrapInfo("... template %d:%s successfully cloned as %d:%s", templateVM.VMID, templateVM.Name, vm.VMID, vm.Name)

	wrapInfo("rendering cloud-init user data")
	userData, err := cloudinit.RenderUserDataTemplate(p.config, agent, cloudinit.RenderOption{})
	if err != nil {
		return wrapError("could not render user data", err)
	}
	wrapInfo("... done")

	// A temporary workaround for the malformed cloud-init template defined in
	// this project's cloudinit pacakge.
	userData = strings.TrimLeft(userData, "\n")

	// A unique instance-id forces cloud-init to treat every clone as a brand new
	// instance and re-run its modules, even when the template image was sealed
	// with stale cloud-init state. newVMID is cluster-unique and atomically
	// reserved, so we'll use that.
	metaData := fmt.Sprintf("instance-id: agent-%d\nlocal-hostname: %s\n", newVMID, agent.Name)

	// CloudInit appends the cloud-init device to the existing boot order. The
	// clone's cached Boot is empty, so seed it with the template's real boot
	// order.
	wrapInfo("attempting to resolve vm %d:%s boot disk", vm.VMID, vm.Name)
	if boot := templateVM.VirtualMachineConfig.Boot; boot == "" {
		disk, err := resolveBootDisk(vm.VirtualMachineConfig)
		if err != nil {
			return wrapError("could not resolve boot disk", err)
		}

		vm.VirtualMachineConfig.Boot = "order=" + disk
	}
	wrapInfo("... boot disk found: %s", vm.VirtualMachineConfig.Boot)

	if err := vm.CloudInit(ctx, "ide2", userData, metaData, "", "", px.WithCloudInitStorage(p.storageISO)); err != nil {
		return wrapError("could not attach user data to vm", err)
	}

	wrapInfo("starting agent vm %d:%s", vm.VMID, vm.Name)
	startTask, err := vm.Start(ctx)
	if err != nil {
		return wrapError("could not start agent vm", err)
	}

	if err := waitFor(ctx, startTask); err != nil {
		return wrapError("agent vm timed out", err)
	}
	wrapInfo("... started")

	wrapInfo("waiting for qemu ready state")
	if err := waitForQemuAgent(ctx, vm, 180*time.Second); err != nil {
		return wrapError("qemu agent timed out", err)
	}
	wrapInfo("... qemu state ready")

	// This will clobber any values derived from the specified template.
	wrapInfo("overriding vm options; tags, description and disk backups")
	vmOptions := []px.VirtualMachineOption{
		px.VirtualMachineOption{Name: "description", Value: agentDescription},
		px.VirtualMachineOption{Name: "tags", Value: agentTag},
	}

	// Ensure we don't accidentally backup these ephemeral agents.
	vmOptions = append(vmOptions, disableDiskBackupOptions(vm.VirtualMachineConfig)...)

	if err := vm.ConfigSync(ctx, vmOptions...); err != nil {
		return wrapError("could not set agent vm options", err)
	}
	wrapInfo("... done")

	return nil
}
