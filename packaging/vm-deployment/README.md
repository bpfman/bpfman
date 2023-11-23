# Developer VM Provisioning

As a developer, if you would like to deploy a VM with BPFMAN running this will spin up the latest Fedora release with BPFMAN installed and running.

### Pre-requisite

- Virtualbox - The vagrant file uses virtualbox for cross-platform support. See [VirtualBox Downloads](https://www.virtualbox.org/wiki/Downloads) and choose your target OS.
- Vagrant - Vagrant deploys the OS and will trigger the Anisble playbook. See [Vagrant Downloads](https://www.vagrantup.com/docs/installation) and choose your target OS.
- Ansible - Once provisioned, Ansible will configure the VM. See [Installing Ansible](https://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html) and choose your target OS.

### Deploy the VM

Once the dependencies are installed, simply clone bpfman and run the following commands:

```console
# Clone the bpfman repo:
$ git clone https://github.com/bpfman/bpfman.git
$ cd bpfman/packaging/vm-deployment/

# Start the vagrant deployment
$ vagrant up

# Once the installation is complete, ssh to the VM
$ vagrant ssh

# View the status of bpfman and run bpfctl
$ sudo systemctl status bpfman
$ sudo bpfctl --help
```
