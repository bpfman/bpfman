# Tutorial

This tutorial will show you how to use `bpfman`.
There are several ways to launch and interact with `bpfman` and `bpfman`:

* **Local Host** - Run `bpfman` as a privileged process straight from build directory.
  See [Local Host](#local-host).
* **Systemd Service** - Run `bpfman` as a systemd service.
  See [Systemd Service](#systemd-service).

## Local Host

### Step 1: Build `bpfman`

Perform the following steps to build `bpfman`.
If this is your first time using bpfman, follow the instructions in
[Setup and Building bpfman](./building-bpfman.md) to setup the prerequisites for building.

```console
cd $HOME/src/bpfman/
cargo xtask build-ebpf --libbpf-dir $HOME/src/libbpf
cargo build
```

### Step 2: Setup `bpfman` environment

`bpfman` supports both communication over a Unix socket.
All examples, both using `bpfman` and the gRPC API use this socket.

### Step 3: Start `bpfman`

While learning and experimenting with `bpfman`, it may be useful to run `bpfman` in the foreground
(which requires a second terminal to run the `bpfman` commands below).
For more details on how logging is handled in bpfman, see [Logging](../developer-guide/logging.md).

```console
sudo RUST_LOG=info ./target/debug/bpfman system service --timeout=0
```

### Step 4: Load your first program

We will load the simple `xdp-pass` program, which permits all traffic to the attached interface,
`vethff657c7` in this example.
The section in the object file that contains the program is "pass".
Finally, we will use the priority of 100.
Find a deeper dive into CLI syntax in [CLI Guide](./cli-guide.md).

```console
sudo ./target/debug/bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface vethff657c7 --priority 100
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6213
 Map Owner ID:  None
 Map Used By:   6213
 Priority:      100
 Iface:         vethff657c7
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6213
 Name:                             pass
 Type:                             xdp
 Loaded At:                        2023-07-17T17:48:10-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [2724]
 BTF ID:                           2834
 Size Translated (bytes):          96
 JITed:                            true
 Size JITed (bytes):               67
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

`bpfman load image` returns the same data as a `bpfman get` command.
From the output, the id of `6213` can be found in the `Kernel State` section.
This id can be used to perform a `bpfman get` to retrieve all relevant program
data and a `bpfman unload` when the program needs to be unloaded.

```console
sudo ./target/debug/bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400
```

We can recheck the details about the loaded program with the `bpfman get` command:

```console
sudo ./target/debug/bpfman get 6213
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6213
 Map Owner ID:  None
 Map Used By:   6213
 Priority:      100
 Iface:         vethff657c7
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6213
 Name:                             pass
 Type:                             xdp
 Loaded At:                        2023-07-17T17:48:10-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [2724]
 BTF ID:                           2834
 Size Translated (bytes):          96
 JITed:                            true
 Size JITed (bytes):               67
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

From the output above you can see the program was loaded to position 0 on our
interface and thus will be executed first.

### Step 5: Loading more programs

We will now load 2 more programs with different priorities to demonstrate how bpfman will ensure they are ordered correctly:

```console
sudo ./target/debug/bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface vethff657c7 --priority 50
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6215
 Map Owner ID:  None
 Map Used By:   6215
 Priority:      50
 Iface:         vethff657c7
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6215
 Name:                             pass
 Type:                             xdp
:
```

```console
sudo ./target/debug/bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface vethff657c7 --priority 200
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6217
 Map Owner ID:  None
 Map Used By:   6217
 Priority:      200
 Iface:         vethff657c7
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6217
 Name:                             pass
 Type:                             xdp
:
```

Using `bpfman list` we can see all the programs that were loaded.

```console
sudo ./target/debug/bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400
 6215        pass  xdp   2023-07-17T17:52:46-0400
 6217        pass  xdp   2023-07-17T17:53:57-0400
```

The lowest priority program is executed first, while the highest is executed last.
As can be seen from the detailed output for each command below:

* Program `6215` is at position `0` with a priority of `50`
* Program `6213` is at position `1` with a priority of `100`
* Program `6217` is at position `2` with a priority of `200`

```console
sudo ./target/debug/bpfman get 6213
 Bpfman State
---------------
 Name:          pass
:
 Priority:      100
 Iface:         vethff657c7
 Position:      1
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6213
 Name:                             pass
 Type:                             xdp
:
```

```console
sudo ./target/debug/bpfman get 6215
 Bpfman State
---------------
 Name:          pass
:
 Priority:      50
 Iface:         vethff657c7
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6215
 Name:                             pass
 Type:                             xdp
:
```

```console
sudo ./target/debug/bpfman get 6217
 Bpfman State
---------------
 Name:          pass
:
 Priority:      200
 Iface:         vethff657c7
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6217
 Name:                             pass
 Type:                             xdp
:
```

By default, the next program in the chain will only be executed if a given program returns
`pass` (see `proceed-on` field in the `bpfman get` output above).
If the next program in the chain should be called even if a different value is returned,
then the program can be loaded with those additional return values using the `proceed-on`
parameter (see `bpfman load image xdp --help` for list of valid values):

```console
sudo ./target/debug/bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface vethff657c7 --priority 150 --proceed-on "pass" --proceed-on "dispatcher_return"
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6219
 Map Owner ID:  None
 Map Used By:   6219
 Priority:      150
 Iface:         vethff657c7
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6219
 Name:                             pass
 Type:                             xdp
:
```

Which results in being loaded in position `2` because it was loaded at priority `150`,
which is lower than the previous program at that position with a priority of `200`.

### Step 6: Delete a program

Let's remove the program at position 1.

```console
sudo ./target/debug/bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400
 6215        pass  xdp   2023-07-17T17:52:46-0400
 6217        pass  xdp   2023-07-17T17:53:57-0400
 6219        pass  xdp   2023-07-17T17:59:41-0400
```

```console
sudo ./target/debug/bpfman unload 6213
```

And we can verify that it has been removed and the other programs re-ordered:

```console
sudo ./target/debug/bpfman list
 Program ID  Name  Type  Load Time
 6215        pass  xdp   2023-07-17T17:52:46-0400
 6217        pass  xdp   2023-07-17T17:53:57-0400
 6219        pass  xdp   2023-07-17T17:59:41-0400
```

```console
./target/debug/bpfman get 6215
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6215
 Map Owner ID:  None
 Map Used By:   6215
 Priority:      50
 Iface:         vethff657c7
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6215
 Name:                             pass
 Type:                             xdp
:
```

```
./target/debug/bpfman get 6217
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6217
 Map Owner ID:  None
 Map Used By:   6217
 Priority:      200
 Iface:         vethff657c7
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6217
 Name:                             pass
 Type:                             xdp
:
```

```
./target/debug/bpfman get 6219
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6219
 Map Owner ID:  None
 Map Used By:   6219
 Priority:      150
 Iface:         vethff657c7
 Position:      1
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 ID:                               6219
 Name:                             pass
 Type:                             xdp
:
```

When `bpfman` is stopped, all remaining programs will be unloaded automatically.

### Step 7: Clean-up

To unwind all the changes, stop `bpfman` and then run the following script:

```console
sudo ./scripts/setup.sh uninstall
```

**WARNING:** `setup.sh uninstall` cleans everything up, so `/etc/bpfman/programs.d/`
and `/run/bpfman/bytecode/` are deleted. Save any changes or files that were created if needed.

## Systemd Service

To run `bpfman` as a systemd service, the binaries will be placed in a well known location
(`/usr/sbin/.`) and a service configuration file will be added
(`/usr/lib/systemd/system/bpfman.service`).
When run as a systemd service, the set of linux capabilities are limited to only the needed set.
If permission errors are encountered, see [Linux Capabilities](../developer-guide/linux-capabilities.md)
for help debugging.

### Step 1

Same as Step 1 above, build `bpfman` if needed:

```console
cd $HOME/src/bpfman/
cargo xtask build-ebpf --libbpf-dir $HOME/src/libbpf
cargo build
```

### Step 2: Setup `bpfman` environment

Run the following command to copy the `bpfman` and `bpfman` binaries to `/usr/sbin/` and copy a
default `bpfman.service` file to `/usr/lib/systemd/system/`.
This option will also start the systemd service `bpfman.service` by default:

```console
sudo ./scripts/setup.sh install
```

> **_NOTE:_** Prior to **kernel 5.19**, all eBPF sys calls required CAP_BPF, which are used to access maps shared
between the BFP program and the userspace program.
So userspace programs that are accessing maps and running on kernels older than 5.19 will require either `sudo`
or the CAP_BPF capability (`sudo /sbin/setcap cap_bpf=ep ./<USERSPACE-PROGRAM>`).

To update the configuration settings associated with running `bpfman` as a service, edit the
service configuration file:

```console
sudo vi /usr/lib/systemd/system/bpfman.service
sudo systemctl daemon-reload
```

If `bpfman` or `bpfman` is rebuilt, the following command can be run to install the update binaries
without regenerating the certifications.
The `bpfman` service will is automatically restarted.

```console
sudo ./scripts/setup.sh reinstall
```

### Step 3: Start `bpfman`

To manage `bpfman` as a systemd service, use `systemctl`. `sudo ./scripts/setup.sh install` will start the service,
but the service can be manually stopped and started:

```console
sudo systemctl stop bpfman.service
...
sudo systemctl start bpfman.service
```

### Step 4-6

Same as above except `bpfman` is now in $PATH:

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface vethff657c7 --priority 100
:


sudo bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400


sudo bpfman unload 6213
```

### Step 7: Clean-up

To unwind all the changes performed while running `bpfman` as a systemd service, run the following
script. This command cleans up everything, including stopping the `bpfman` service if it is still
running.

```console
sudo ./scripts/setup.sh uninstall
```

**WARNING:** `setup.sh uninstall` cleans everything up, so `/etc/bpfman/programs.d/`
and `/run/bpfman/bytecode/` are deleted. Save any changes or files that were created if needed.


## Build and Run Local eBPF Programs

In the examples above, all the eBPF programs were pulled from pre-built images.
This tutorial uses examples from the [xdp-tutorial](https://github.com/xdp-project/xdp-tutorial).
The pre-built container images can be found here:
[https://quay.io/organization/bpfman-bytecode](https://quay.io/organization/bpfman-bytecode)

To build these examples locally, check out the
[xdp-tutorial](https://github.com/xdp-project/xdp-tutorial) git repository and
compile the examples.
[eBPF Bytecode Image Specifications](../developer-guide/shipping-bytecode.md) describes how eBPF
bytecode ispackaged in container images.

To load these programs locally, use the `bpfman load file` command in place of the
`bpfman load image` command.
For example:

```console
sudo ./target/debug/bpfman load file --path /$HOME/src/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o --name "pass" xdp --iface vethff657c7 --priority 100
```
