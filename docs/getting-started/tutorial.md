# Tutorial

This tutorial will show you how to use `bpfd`.
There are several ways to launch and interact with `bpfd` and `bpfctl`:

* **Privileged Mode** - Run `bpfd` as a privileged process straight from build directory.
  `bpfd` user is not created so `sudo` is always required when executing `bpfctl` commands.
  See [Privileged Mode](#privileged-mode).
* **Systemd Service** - Run `bpfd` as a systemd service as the `bpfd` user.
  See [Systemd Service](#systemd-service).

## Privileged Mode

### Step 1: Build `bpfd`

Perform the following steps to build `bpfd`.
If this is your first time using bpfd, follow the instructions in
[Setup and Building bpfd](./building-bpfd.md) to setup the prerequisites for building.

```console
cd $HOME/src/bpfd/
cargo xtask build-ebpf --libbpf-dir $HOME/src/libbpf
cargo build
```

### Step 2: Setup `bpfd` environment

`bpfd` supports both mTLS for mutual authentication with clients and connecting via a Unix socket.
This tutorial will be using `bpfctl`, which sends gRPC requests to `bpfd` over a Unix socket.
In the [Example eBPF Programs](./example-bpf.md), the GO examples use mTLS over TCP to interact
with `bpfd`.
If no local certificate authority exists when `bpfd` is started, `bpfd` will automatically
create the certificate authority in `/etc/bpfd/certs/`.
For this step, no additional actions need to be taken.

### Step 3: Start `bpfd`

While learning and experimenting with `bpfd`, it may be useful to run `bpfd` in the foreground
(which requires a second terminal to run the `bpfctl` commands below).
For more details on how logging is handled in bpfd, see [Logging](../developer-guide/logging.md).

```console
sudo RUST_LOG=info ./target/debug/bpfd
```

### Step 4: Load your first program

We will load the simple `xdp-pass` program, which permits all traffic to the attached interface,
`vethb2795c7` in this example.
The section in the object file that contains the program is "xdp".
Finally, we will use the priority of 100.
Find a deeper dive into `bpfctl` syntax in [bpfctl Guide](./bpfctl-guide.md).

```console
sudo ./target/debug/bpfctl load-from-image --image-url quay.io/bpfd-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 100
92e3e14c-0400-4a20-be2d-f701af21873c
```

`bpfctl` returns a unique identifier (`92e3e14c-0400-4a20-be2d-f701af21873c` in this example) to the program that was loaded.
This may be used to detach the program later.
We can check the program was loaded using the following command:

```console
sudo ./target/debug/bpfctl list
 UUID                                  Type  Name  Location                                                                         Metadata
 92e3e14c-0400-4a20-be2d-f701af21873c  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 100, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }
```

From the output above you can see the program was loaded to position 0 on our interface and will be executed first.

### Step 5: Loading more programs

We will now load 2 more programs with different priorities to demonstrate how bpfd will ensure they are ordered correctly:

```console
sudo ./target/debug/bpfctl load-from-image --image-url quay.io/bpfd-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 50
1ccc1376-60e8-4dc5-9079-6c32748fa1c4
```

```console
sudo ./target/debug/bpfctl load-from-image --image-url quay.io/bpfd-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 200
6af7c28f-6a7f-46ee-bc98-2d92ed261369
```

Using `bpfctl list` we can see that the programs are correctly ordered.
The lowest priority program is executed first, while the highest is executed last.

```console
sudo ./target/debug/bpfctl list
 UUID                                  Type  Name  Location                                                                         Metadata
 1ccc1376-60e8-4dc5-9079-6c32748fa1c4  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 50, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }
 92e3e14c-0400-4a20-be2d-f701af21873c  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 100, iface: vethb2795c7, position: 1, proceed_on: pass, dispatcher_return }
 6af7c28f-6a7f-46ee-bc98-2d92ed261369  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 200, iface: vethb2795c7, position: 2, proceed_on: pass, dispatcher_return }
```

By default, the next program in the chain will only be executed if a given program returns
`pass` (see `proceed-on` field in the `bpfctl list` output above).
If the next program in the chain should be called even if a different value is returned,
then the program can be loaded with those additional return values using the `proceed-on`
parameter (see `bpfctl load-from-image xdp --help` for list of valid values):

```console
sudo ./target/debug/bpfctl load-from-image --image-url quay.io/bpfd-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 150 --proceed-on "pass" --proceed-on "dispatcher_return"
b2f19b7b-4c71-4338-873e-914bd8fa44ba
```

Which results in (see position 2):

```console
sudo ./target/debug/bpfctl list
 UUID                                  Type  Name  Location                                                                         Metadata
 1ccc1376-60e8-4dc5-9079-6c32748fa1c4  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 50, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }
 92e3e14c-0400-4a20-be2d-f701af21873c  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 100, iface: vethb2795c7, position: 1, proceed_on: pass, dispatcher_return }
 b2f19b7b-4c71-4338-873e-914bd8fa44ba  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 150, iface: vethb2795c7, position: 2, proceed_on: pass, dispatcher_return }
 6af7c28f-6a7f-46ee-bc98-2d92ed261369  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 200, iface: vethb2795c7, position: 3, proceed_on: pass, dispatcher_return }
```

> **_NOTE:_**  The list of programs may not always be sorted in the order of execution.
The `position` indicates the order of execution, low to high.

### Step 6: Delete a program

Let's remove the program at position 1.

```console
sudo ./target/debug/bpfctl unload 92e3e14c-0400-4a20-be2d-f701af21873c
```

And we can verify that it has been removed and the other programs re-ordered:

```console
sudo ./target/debug/bpfctl list
 UUID                                  Type  Name  Location                                                                         Metadata
 1ccc1376-60e8-4dc5-9079-6c32748fa1c4  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 50, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }
 b2f19b7b-4c71-4338-873e-914bd8fa44ba  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 150, iface: vethb2795c7, position: 1, proceed_on: pass, dispatcher_return }
 6af7c28f-6a7f-46ee-bc98-2d92ed261369  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 200, iface: vethb2795c7, position: 2, proceed_on: pass, dispatcher_return }
```

When `bpfd` is stopped, all remaining programs will be unloaded automatically.

### Step 7: Clean-up

To unwind all the changes, stop `bpfd` and then run the following script:

```console
sudo ./scripts/setup.sh uninstall
```

**WARNING:** `setup.sh uninstall` cleans everything up, so `/etc/bpfd/programs.d/`
and `/run/bpfd/bytecode/` are deleted. Save any changes or files that were created if needed.

## Systemd Service

To run `bpfd` as a systemd service, the binaries will be placed in a well known location
(`/usr/sbin/.`) and a service configuration file will be added
(`/usr/lib/systemd/system/bpfd.service`).
When run as a systemd service, the set of linux capabilities are limited to only the needed set.
If permission errors are encountered, see [Linux Capabilities](../developer-guide/linux-capabilities.md)
for help debugging.

### Step 1

Same as Step 1 above, build `bpfd` if needed:

```console
cd $HOME/src/bpfd/
cargo xtask build-ebpf --libbpf-dir $HOME/src/libbpf
cargo build
```

### Step 2: Setup `bpfd` environment

Run the following command to copy the `bpfd` and `bpfctl` binaries to `/usr/sbin/` and set the user
and user group for each, and copy a default `bpfd.service` file to `/usr/lib/systemd/system/`.
This option will also start the systemd service `bpfd.service` by default:

```console
sudo ./scripts/setup.sh install
```

Then add usergroup `bpfd` to the desired user if not already run and logout/login to apply.
Programs run by users which are members of the `bpfd` user group are able to access the mTLS certificates
created by bpfd.
Therefore, these programs can make bpfd requests without requiring `sudo`.
For userspace programs accessing maps, the maps are owned by the `bpfd` user and `bpfd` user group.
Programs run by users which are members of the `bpfd` user group are able to access the maps files without
requiring  `sudo` (specifically CAP_DAC_SEARCH or CAP_DAC_OVERIDE).

```console
sudo usermod -a -G bpfd $USER
exit
<LOGIN>
```

> **_NOTE:_** Prior to **kernel 5.19**, all eBPF sys calls required CAP_BPF, which are used to access maps shared
between the BFP program and the userspace program.
So userspace programs that are accessing maps and running on kernels older than 5.19 will require either `sudo`
or the CAP_BPF capability (`sudo /sbin/setcap cap_bpf=ep ./<USERSPACE-PROGRAM>`).


To update the configuration settings associated with running `bpfd` as a service, edit the
service configuration file:

```console
sudo vi /usr/lib/systemd/system/bpfd.service
sudo systemctl daemon-reload
```

If `bpfd` or `bpfctl` is rebuilt, the following command can be run to install the update binaries
without tearing down the users and regenerating the certifications.
The `bpfd` service will is automatically restarted.

```console
sudo ./scripts/setup.sh reinstall
```

### Step 3: Start `bpfd`

To manage `bpfd` as a systemd service, use `systemctl`. `sudo ./scripts/setup.sh install` will start the service,
but the service can be manually stopped and started:

```console
sudo systemctl stop bpfd.service
...
sudo systemctl start bpfd.service
```

### Step 4-6

Same as above except `sudo` can be dropped from all the `bpfctl` commands and `bpfctl` is now in $PATH:

```console
bpfctl load-from-image --image-url quay.io/bpfd-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 100
92e3e14c-0400-4a20-be2d-f701af21873c


bpfctl list
 UUID                                  Type  Name  Location                                                                         Metadata
 92e3e14c-0400-4a20-be2d-f701af21873c  xdp   pass  image: { url: quay.io/bpfd-bytecode/xdp_pass:latest, pullpolicy: IfNotPresent }  { priority: 100, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }


bpfctl unload 92e3e14c-0400-4a20-be2d-f701af21873c
```

### Step 7: Clean-up

To unwind all the changes performed while running `bpfd` as a systemd service, run the following
script. This command cleans up everything, including stopping the `bpfd` service if it is still
running.

```console
sudo ./scripts/setup.sh uninstall
```

**WARNING:** `setup.sh uninstall` cleans everything up, so `/etc/bpfd/programs.d/`
and `/run/bpfd/bytecode/` are deleted. Save any changes or files that were created if needed.


## Build and Run Local eBPF Programs

In the examples above, all the eBPF programs were pulled from pre-built images.
This tutorial uses examples from the [xdp-tutorial](https://github.com/xdp-project/xdp-tutorial).
The pre-built container images can be found here:
[https://quay.io/organization/bpfd-bytecode](https://quay.io/organization/bpfd-bytecode)

To build these examples locally, check out the
[xdp-tutorial](https://github.com/xdp-project/xdp-tutorial) git repository and
compile the examples.
[eBPF Bytecode Image Specifications](../developer-guide/shipping-bytecode.md) describes how eBPF
bytecode ispackaged in container images.

To load these programs locally, use the `bpfctl load-from-file` command in place of the
`bpfctl load-from-image` command.
For example:

```console
sudo ./target/debug/bpfctl load-from-file --path /$HOME/src/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o --section-name "xdp" xdp --iface vethb2795c7 --priority 100
```
