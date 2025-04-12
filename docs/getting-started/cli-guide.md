# CLI Guide

`bpfman` offers several CLI commands to interact with the `bpfman` daemon.
The CLI allows you to `load`, `unload`, `get` and `list` eBPF programs.

## Notes For This Guide

As described in other sections, `bpfman` can be run as either a privileged process or
a systemd service.
If run as a privileged process, `bpfman` will most likely be run from your local
development branch and will require `sudo`.
Example:

```console
sudo ./target/debug/bpfman list
```

If run as a systemd service, `bpfman` will most likely be installed in your $PATH,
and will also require `sudo`.
Example:

```console
sudo bpfman list
```

The examples here use `sudo bpfman` in place of `sudo ./target/debug/bpfman` for readability,
use as your system is deployed.

eBPF object files used in the examples are taken from the
[examples](https://github.com/bpfman/bpfman/tree/main/examples) and
[integration-test](https://github.com/bpfman/bpfman/tree/main/tests/integration-test) directories
from the `bpfman` repository.

## Basic Syntax

Below are the commands supported by `bpfman`.

```console
An eBPF manager focusing on simplifying the deployment and administration of eBPF programs.

Usage: bpfman <COMMAND>

Commands:
  load    Load an eBPF program on the system
  unload  Unload an eBPF program using the Program Id
  list    List all eBPF programs loaded via bpfman
  get     Get an eBPF program using the Program Id
  image   eBPF Bytecode Image related commands
  help    Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')
```

## bpfman load

The `bpfman load file` and `bpfman load image` commands are used to load eBPF programs.
The `bpfman load file` command is used to load a locally built eBPF program.
The `bpfman load image` command is used to load an eBPF program packaged in a OCI container
image from a given registry.
Each program type (i.e. `<COMMAND>`) has it's own set of attributes specific to the program type,
and those attributes MUST come after the program type is entered.
There are a common set of attributes, and those MUST come before the program type is entered.

```console
sudo bpfman load file --help
Load an eBPF program from a local .o file

Usage: bpfman load file [OPTIONS] --path <PATH> --name <NAME> <COMMAND>

Commands:
  xdp         Install an eBPF program on the XDP hook point for a given interface
  tc          Install an eBPF program on the TC hook point for a given interface
  tracepoint  Install an eBPF program on a Tracepoint
  kprobe      Install a kprobe or kretprobe eBPF probe
  uprobe      Install a uprobe or uretprobe eBPF probe
  fentry      Install a fentry eBPF probe
  fexit       Install a fexit eBPF probe
  help        Print this message or the help of the given subcommand(s)

Options:
  -p, --path <PATH>
          Required: Location of local bytecode file
          Example: --path /run/bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o

  -n, --name <NAME>
          Required: The name of the function that is the entry point for the BPF program

  -g, --global <GLOBAL>...
          Optional: Global variables to be set when program is loaded.
          Format: <NAME>=<Hex Value>

          This is a very low level primitive. The caller is responsible for formatting
          the byte string appropriately considering such things as size, endianness,
          alignment and packing of data structures.

  -m, --metadata <METADATA>
          Optional: Specify Key/Value metadata to be attached to a program when it
          is loaded by bpfman.
          Format: <KEY>=<VALUE>

          This can later be used to `list` a certain subset of programs which contain
          the specified metadata.
          Example: --metadata owner=acme

      --map-owner-id <MAP_OWNER_ID>
          Optional: Program Id of loaded eBPF program this eBPF program will share a map with.
          Only used when multiple eBPF programs need to share a map.
          Example: --map-owner-id 63178

  -h, --help
          Print help (see a summary with '-h')
```

and

```console
sudo bpfman load image --help
Load an eBPF program packaged in a OCI container image from a given registry

Usage: bpfman load image [OPTIONS] --image-url <IMAGE_URL> --name <NAME> <COMMAND>

Commands:
  xdp         Install an eBPF program on the XDP hook point for a given interface
  tc          Install an eBPF program on the TC hook point for a given interface
  tracepoint  Install an eBPF program on a Tracepoint
  kprobe      Install a kprobe or kretprobe eBPF probe
  uprobe      Install a uprobe or uretprobe eBPF probe
  fentry      Install a fentry eBPF probe
  fexit       Install a fexit eBPF probe
  help        Print this message or the help of the given subcommand(s)

Options:
  -i, --image-url <IMAGE_URL>
          Required: Container Image URL.
          Example: --image-url quay.io/bpfman-bytecode/xdp_pass:latest

  -r, --registry-auth <REGISTRY_AUTH>
          Optional: Registry auth for authenticating with the specified image registry.
          This should be base64 encoded from the '<username>:<password>' string just like
          it's stored in the docker/podman host config.
          Example: --registry_auth "YnjrcKw63PhDcQodiU9hYxQ2"

  -p, --pull-policy <PULL_POLICY>
          Optional: Pull policy for remote images.

          [possible values: Always, IfNotPresent, Never]

          [default: IfNotPresent]

  -n, --name <NAME>
          Required: The name of the function that is the entry point for the eBPF program.

  -g, --global <GLOBAL>...
          Optional: Global variables to be set when program is loaded.
          Format: <NAME>=<Hex Value>

          This is a very low level primitive. The caller is responsible for formatting
          the byte string appropriately considering such things as size, endianness,
          alignment and packing of data structures.

  -m, --metadata <METADATA>
          Optional: Specify Key/Value metadata to be attached to a program when it
          is loaded by bpfman.
          Format: <KEY>=<VALUE>

          This can later be used to list a certain subset of programs which contain
          the specified metadata.
          Example: --metadata owner=acme

      --map-owner-id <MAP_OWNER_ID>
          Optional: Program Id of loaded eBPF program this eBPF program will share a map with.
          Only used when multiple eBPF programs need to share a map.
          Example: --map-owner-id 63178

  -h, --help
          Print help (see a summary with '-h')
```

When using either load command, `--path`, `--image-url`, `--registry-auth`, `--pull-policy`, `--name`,
 `--global`, `--metadata` and `--map-owner-id` must be entered before the `<COMMAND>` (`xdp`, `tc`,
 `tracepoint`, etc) is entered.
Then each `<COMMAND>` has its own custom parameters (same for both `bpfman load file` and
`bpfman load image`):

```console
sudo bpfman load file xdp --help
Install an eBPF program on the XDP hook point for a given interface

Usage: bpfman load file --path <PATH> --name <NAME> xdp [OPTIONS] --iface <IFACE> --priority <PRIORITY>

Options:
  -i, --iface <IFACE>
          Required: Interface to load program on

  -p, --priority <PRIORITY>
          Required: Priority to run program in chain. Lower value runs first

      --proceed-on <PROCEED_ON>...
          Optional: Proceed to call other programs in chain on this exit code.
          Multiple values supported by repeating the parameter.
          Example: --proceed-on "pass" --proceed-on "drop"

          [possible values: aborted, drop, pass, tx, redirect, dispatcher_return]

          [default: pass, dispatcher_return]

  -h, --help
          Print help (see a summary with '-h')
```

Example loading from local file (`--path` is the fully qualified path):

```console
cd bpfman/
sudo bpfman load file --path tests/integration-test/bpf/.output/xdp_pass.bpf.o --name "pass" xdp --iface eno3 --priority 100
```

Example from image in remote repository:

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --name "pass" xdp --iface eno3 --priority 100
```

The `tc` command is similar to `xdp`, but it also requires the `direction` option
and the `proceed-on` values are different.

```console
sudo bpfman load file tc --help
Install an eBPF program on the TC hook point for a given interface

Usage: bpfman load file --path <PATH> --name <NAME> tc [OPTIONS] --direction <DIRECTION> --iface <IFACE> --priority <PRIORITY>

Options:
  -d, --direction <DIRECTION>
          Required: Direction to apply program.

          [possible values: ingress, egress]

  -i, --iface <IFACE>
          Required: Interface to load program on

  -p, --priority <PRIORITY>
          Required: Priority to run program in chain. Lower value runs first

      --proceed-on <PROCEED_ON>...
          Optional: Proceed to call other programs in chain on this exit code.
          Multiple values supported by repeating the parameter.
          Example: --proceed-on "ok" --proceed-on "pipe"

          [possible values: unspec, ok, reclassify, shot, pipe, stolen, queued,
                            repeat, redirect, trap, dispatcher_return]

          [default: ok, pipe, dispatcher_return]

  -h, --help
          Print help (see a summary with '-h')
```

The following is an example of the `tc` command using short option names:

```console
cd bpfman/
sudo bpfman load file -p tests/integration-test/bpf/.output/tc_pass.bpf.o -n "pass" tc -d ingress -i mynet1 -p 40
```

For the `tc_pass.bpf.o` program loaded with the command above, the name
would be set as shown in the following snippet, taken from the function name, not `SEC()`:

```c
SEC("classifier/pass")
int pass(struct __sk_buff *skb) {
{
    :
}
```

### Additional Load Examples

Below are some additional examples of `bpfman load` commands:

#### Fentry

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/fentry:latest --name "test_fentry" fentry -f do_unlinkat
```

#### Fexit

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/fexit:latest --name "test_fexit" fexit -f do_unlinkat
```

#### Kprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/kprobe:latest --name "my_kprobe" kprobe -f try_to_wake_up
```

#### Kretprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/kretprobe:latest --name "my_kretprobe" kprobe -f try_to_wake_up -r
```

#### TC

```console
cd bpfman/
sudo bpfman load file --path examples/go-tc-counter/bpf_x86_bpfel.o --name "stats" tc --direction ingress --iface eno3 --priority 110
```

#### Uprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/uprobe:latest --name "my_uprobe" uprobe -f "malloc" -t "libc"
```

#### Uretprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/uretprobe:latest --name "my_uretprobe" uprobe -f "malloc" -t "libc" -r
```

#### XDP

```console
cd bpfman/
sudo bpfman load file --path bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o --name "xdp_stats" xdp --iface eno3 --priority 35
```

### Setting Global Variables in eBPF Programs

Global variables can be set for any eBPF program type when loading as follows:

```console
cd bpfman/
sudo bpfman load file -p bpfman/tests/integration-test/bpf/.output/tc_pass.bpf.o -g GLOBAL_u8=01 GLOBAL_u32=0A0B0C0D -n "pass" tc -d ingress -i mynet1 -p 40
```

Note that when setting global variables, the eBPF program being loaded must
have global variables named with the strings given, and the size of the value
provided must match the size of the given variable.  For example, the above
command can be used to update the following global variables in an eBPF program.

```c
volatile const __u32 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;
```

### Modifying the Proceed-On Behavior

The `proceed-on` setting applies to `xdp` and `tc` programs. For both of these
program types, an ordered list of eBPF programs is maintained per attach point.
The `proceed-on` setting determines whether processing will "proceed" to the
next eBPF program in the list, or terminate processing and return, based on the
program's return value. For example, the default `proceed-on` configuration for
an `xdp` program can be modified as follows:

```console
cd bpfman/
sudo bpfman load file -p tests/integration-test/bpf/.output/xdp_pass.bpf.o -n "pass" xdp -i mynet1 -p 30 --proceed-on drop pass dispatcher_return
```

### Sharing Maps Between eBPF Programs

!!! Warning
    Currently for the map sharing feature to work the LIBBPF_PIN_BY_NAME flag **MUST** be set in
    the shared bpf map definitions.
    Please see [this aya issue](https://github.com/aya-rs/aya/issues/837) for future work that will
    change this requirement.

To share maps between eBPF programs, first load the eBPF program that owns the
maps.
One eBPF program must own the maps.

```console
cd bpfman/
sudo bpfman load file --path examples/go-xdp-counter/bpf_x86_bpfel.o -n "xdp_stats" xdp --iface eno3 --priority 100
6371
```

Next, load additional eBPF programs that will share the existing maps by passing
the program id of the eBPF program that owns the maps using the `--map-owner-id`
parameter:

```console
cd bpfman/
sudo bpfman load file --path examples/go-xdp-counter/bpf_x86_bpfel.o -n "xdp_stats" --map-owner-id 6371 xdp --iface eno3 --priority 100
6373
```

Use the `bpfman get <PROGRAM_ID>` command to display the configuration:

```console
sudo bpfman list
 Program ID  Name       Type  Load Time
 6371        xdp_stats  xdp   2023-07-18T16:50:46-0400
 6373        xdp_stats  xdp   2023-07-18T16:51:06-0400
```

```console
sudo bpfman get 6371
 Bpfman State
---------------
 Name:          xdp_stats
 Path:          /home/<$USER>/src/bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6371
 Map Owner ID:  None
 Map Used By:   6371
                6373
 Priority:      100
 Iface:         eno3
 Position:      1
 Proceed On:    pass, dispatcher_return
:
```

```console
sudo bpfman get 6373
 Bpfman State
---------------
 Name:          xdp_stats
 Path:          /home/<$USER>/src/bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6371
 Map Owner ID:  6371
 Map Used By:   6371
                6373
 Priority:      100
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return
:
```

As the output shows, the first program (`6371`) owns the map, with `Map Owner ID`
of `None` and the `Map Pin Path` (`/run/bpfman/fs/maps/6371`) that includes its own ID.

The second program (`6373`) references the first program via the `Map Owner ID` set
to `6371` and the `Map Pin Path` (`/run/bpfman/fs/maps/6371`) set to same directory as
the first program, which includes the first program's ID.
The output for both commands shows the map is being used by both programs via
the `Map Used By` with values of `6371` and `6373`.

The eBPF programs can be unloaded any order, the `Map Pin Path` will not be deleted
until all the programs referencing the maps are unloaded:

```console
sudo bpfman unload 6371
sudo bpfman unload 6373
```

## bpfman list

The `bpfman list` command lists all the bpfman loaded eBPF programs:

```console
sudo bpfman list
 Program ID  Name              Type        Load Time
 6201        pass              xdp         2023-07-17T17:17:53-0400
 6202        sys_enter_openat  tracepoint  2023-07-17T17:19:09-0400
 6204        stats             tc          2023-07-17T17:20:14-0400
```

To see all eBPF programs loaded on the system, include the `--all` option.

```console
sudo bpfman list --all
 Program ID  Name              Type           Load Time
 52          restrict_filesy   lsm            2023-05-03T12:53:34-0400
 166         dump_bpf_map      tracing        2023-05-03T12:53:52-0400
 167         dump_bpf_prog     tracing        2023-05-03T12:53:52-0400
 455                           cgroup_device  2023-05-03T12:58:26-0400
 :
 6194                          cgroup_device  2023-07-17T17:15:23-0400
 6201        pass              xdp            2023-07-17T17:17:53-0400
 6202        sys_enter_openat  tracepoint     2023-07-17T17:19:09-0400
 6203        dispatcher        tc             2023-07-17T17:20:14-0400
 6204        stats             tc             2023-07-17T17:20:14-0400
 6207        xdp               xdp            2023-07-17T17:27:13-0400
 6210        test_fentry       tracing        2023-07-17T17:28:34-0400
 6212        test_fexit        tracing        2023-07-17T17:29:02-0400
 6223        my_uprobe         probe          2023-07-17T17:31:45-0400
 6225        my_kretprobe      probe          2023-07-17T17:32:27-0400
 6928        my_kprobe         probe          2023-07-17T17:33:49-0400
```

To filter on a given program type, include the `--program-type` parameter:

```console
sudo bpfman list --all --program-type tc
 Program ID  Name        Type  Load Time
 6203        dispatcher  tc    2023-07-17T17:20:14-0400
 6204        stats       tc    2023-07-17T17:20:14-0400
```

Note: The list filters by the Kernel Program Type.
`kprobe`, `kretprobe`, `uprobe` and `uretprobe` all map to the `probe` Kernel Program Type.
`fentry` and `fexit` both map to the `tracing` Kernel Program Type.

## bpfman get

To retrieve detailed information for a loaded eBPF program, use the
`bpfman get <PROGRAM_ID>` command.
If the eBPF program was loaded via bpfman, then there will be a `Bpfman State`
section with bpfman related attributes and a `Kernel State` section with
kernel information.
If the eBPF program was loaded outside of bpfman, then the `Bpfman State`
section will be empty and `Kernel State` section will be populated.

```console
sudo bpfman get 6204
 Bpfman State
---------------
 Name:          stats
 Image URL:     quay.io/bpfman-bytecode/go-tc-counter:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6204
 Map Owner ID:  None
 Map Used By:   6204
 Priority:      100
 Iface:         eno3
 Position:      0
 Direction:     eg
 Proceed On:    pipe, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6204
 Name:                             stats
 Type:                             tc
 Loaded At:                        2023-07-17T17:20:14-0400
 Tag:                              ead94553702a3742
 GPL Compatible:                   true
 Map IDs:                          [2705]
 BTF ID:                           2821
 Size Translated (bytes):          176
 JITed:                            true
 Size JITed (bytes):               116
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       24
```

```console
sudo bpfman get 6190
 Bpfman State
---------------
NONE

 Kernel State
----------------------------------
Program ID:                        6190
Name:                              None
Type:                              cgroup_skb
Loaded At:                         2023-07-17T17:15:23-0400
Tag:                               6deef7357e7b4530
GPL Compatible:                    true
Map IDs:                           []
BTF ID:                            0
Size Translated (bytes):           64
JITed:                             true
Size JITed:                        55
Kernel Allocated Memory (bytes):   4096
Verified Instruction Count:        8
```

## bpfman unload

The `bpfman unload` command takes the program id from the load or list command as a parameter,
and unloads the requested eBPF program:

```console
sudo bpfman unload 6204
```

```console
sudo bpfman list
 Program ID  Name              Type        Load Time
 6201        pass              xdp         2023-07-17T17:17:53-0400
 6202        sys_enter_openat  tracepoint  2023-07-17T17:19:09-0400
```

## bpfman image

The `bpfman image` commands contain a set of container image related commands.

### bpfman image pull

The `bpfman image pull` command pulls a given bytecode image for future use
by a load command.

```console
sudo bpfman image pull --help
Pull an eBPF bytecode image from a remote registry

Usage: bpfman image pull [OPTIONS] --image-url <IMAGE_URL>

Options:
  -i, --image-url <IMAGE_URL>
          Required: Container Image URL.
          Example: --image-url quay.io/bpfman-bytecode/xdp_pass:latest

  -r, --registry-auth <REGISTRY_AUTH>
          Optional: Registry auth for authenticating with the specified image registry.
          This should be base64 encoded from the '<username>:<password>' string just like
          it's stored in the docker/podman host config.
          Example: --registry_auth "YnjrcKw63PhDcQodiU9hYxQ2"

  -p, --pull-policy <PULL_POLICY>
          Optional: Pull policy for remote images.

          [possible values: Always, IfNotPresent, Never]

          [default: IfNotPresent]

  -h, --help
          Print help (see a summary with '-h')
```

Example usage:

```console
sudo bpfman image pull --image-url quay.io/bpfman-bytecode/xdp_pass:latest
Successfully downloaded bytecode
```

Then when loaded, the local image will be used:

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --pull-policy IfNotPresent xdp --iface eno3 --priority 100
 Bpfman State                                           
 ---------------
 Name:          pass                                  
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest 
 Pull Policy:   IfNotPresent                          
 Global:        None                                  
 Metadata:      None                                  
 Map Pin Path:  /run/bpfman/fs/maps/406681              
 Map Owner ID:  None                                  
 Maps Used By:  None                                  
 Priority:      100                                   
 Iface:         eno3
 Position:      2                                     
 Proceed On:    pass, dispatcher_return               

 Kernel State                                               
 ----------------------------------
 Program ID:                       406681                   
 Name:                             pass                     
 Type:                             xdp                      
 Loaded At:                        1917-01-27T01:37:06-0500 
 Tag:                              4b9d1b2c140e87ce         
 GPL Compatible:                   true                     
 Map IDs:                          [736646]                 
 BTF ID:                           555560                   
 Size Translated (bytes):          96                       
 JITted:                           true                     
 Size JITted:                      67                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       9                        
```

### bpfman image build

The `bpfman image build` command is a utility command that builds and pushes an eBPF program
in a OCI container image leveraging either `docker` or `podman`.
The eBPF program bytecode must already be generated.
This command calls `docker` or `podman` with the proper parameters for building
multi-architecture based images with the proper labels for a OCI container image.

Since this command is leveraging `docker` and `podman`, a container file (`--container-file` or `-f`)
is required, along with an image tag (`--tag` of `-t`).
In addition, the bytecode to package must be included.
The bytecode can take several forms, but at least one must be provided:

* `--bytecode` or `-b`: Use this option for a single bytecode object file built for the host architecture.
  The value of this parameter is a single bytecode object file.
* `--cilium-ebpf-project` or `-c`: Use this option for a cilium/ebpf based project.
  The value of this parameter is a directory that contains multiple object files for different architectures,
  where the object files follow the Cilium naming convention with the architecture in the name (i.e. bpf_x86_bpfel.o,
  bpf_arm64_bpfel.o, bpf_powerpc_bpfel.o, bpf_s390_bpfeb.o).
* `--bc-386-el` .. `--bc-s390x-eb`: Use this option to add one or more architecture specific bytecode files.

```console
bpfman image build --help
Build an eBPF bytecode image from local bytecode objects and push to a registry.

To use, the --container-file and --tag must be included, as well as a pointer to
at least one bytecode file that can be passed in several ways. Use either:

* --bytecode: for a single bytecode built for the host architecture.

* --cilium-ebpf-project: for a cilium/ebpf project directory which contains
    multiple object files for different architectures.

* --bc-386-el .. --bc-s390x-eb: to add one or more architecture specific bytecode files.

Examples:
   bpfman image build -f Containerfile.bytecode -t quay.io/<USER>/go-xdp-counter:test \
     -b ./examples/go-xdp-counter/bpf_x86_bpfel.o

Usage: bpfman image build [OPTIONS] --tag <TAG> --container-file <CONTAINER_FILE> <--bytecode <BYTECODE>|--cilium-ebpf-project <CILIUM_EBPF_PROJECT>|--bc-386-el <BC_386_EL>|--bc-amd64-el <BC_AMD64_EL>|--bc-arm-el <BC_ARM_EL>|--bc-arm64-el <BC_ARM64_EL>|--bc-loong64-el <BC_LOONG64_EL>|--bc-mips-eb <BC_MIPS_EB>|--bc-mipsle-el <BC_MIPSLE_EL>|--bc-mips64-eb <BC_MIPS64_EB>|--bc-mips64le-el <BC_MIPS64LE_EL>|--bc-ppc64-eb <BC_PPC64_EB>|--bc-ppc64le-el <BC_PPC64LE_EL>|--bc-riscv64-el <BC_RISCV64_EL>|--bc-s390x-eb <BC_S390X_EB>>

Options:
  -t, --tag <TAG>
          Required: Name and optionally a tag in the name:tag format.
          Example: --tag quay.io/bpfman-bytecode/xdp_pass:latest

  -f, --container-file <CONTAINER_FILE>
          Required: Dockerfile to use for building the image.
          Example: --container_file Containerfile.bytecode

  -r, --runtime <RUNTIME>
          Optional: Container runtime to use, works with docker or podman, defaults to docker
          Example: --runtime podman

  -b, --bytecode <BYTECODE>
          Optional: bytecode file to use for building the image assuming host architecture.
          Example: -b ./examples/go-xdp-counter/bpf_x86_bpfel.o

  -c, --cilium-ebpf-project <CILIUM_EBPF_PROJECT>
          Optional: If specified pull multi-arch bytecode files from a cilium/ebpf formatted project
          where the bytecode files all contain a standard bpf_<GOARCH>_<(el/eb)>.o tag.
          Example: --cilium-ebpf-project ./examples/go-xdp-counter

      --bc-386-el <BC_386_EL>
          Optional: bytecode file to use for building the image assuming amd64 architecture.
          Example: --bc-386-el ./examples/go-xdp-counter/bpf_386_bpfel.o

      --bc-amd64-el <BC_AMD64_EL>
          Optional: bytecode file to use for building the image assuming amd64 architecture.
          Example: --bc-amd64-el ./examples/go-xdp-counter/bpf_x86_bpfel.o

      --bc-arm-el <BC_ARM_EL>
          Optional: bytecode file to use for building the image assuming arm architecture.
          Example: --bc-arm-el ./examples/go-xdp-counter/bpf_arm_bpfel.o

      --bc-arm64-el <BC_ARM64_EL>
          Optional: bytecode file to use for building the image assuming arm64 architecture.
          Example: --bc-arm64-el ./examples/go-xdp-counter/bpf_arm64_bpfel.o

      --bc-loong64-el <BC_LOONG64_EL>
          Optional: bytecode file to use for building the image assuming loong64 architecture.
          Example: --bc-loong64-el ./examples/go-xdp-counter/bpf_loong64_bpfel.o

      --bc-mips-eb <BC_MIPS_EB>
          Optional: bytecode file to use for building the image assuming mips architecture.
          Example: --bc-mips-eb ./examples/go-xdp-counter/bpf_mips_bpfeb.o

      --bc-mipsle-el <BC_MIPSLE_EL>
          Optional: bytecode file to use for building the image assuming mipsle architecture.
          Example: --bc-mipsle-el ./examples/go-xdp-counter/bpf_mipsle_bpfel.o

      --bc-mips64-eb <BC_MIPS64_EB>
          Optional: bytecode file to use for building the image assuming mips64 architecture.
          Example: --bc-mips64-eb ./examples/go-xdp-counter/bpf_mips64_bpfeb.o

      --bc-mips64le-el <BC_MIPS64LE_EL>
          Optional: bytecode file to use for building the image assuming mips64le architecture.
          Example: --bc-mips64le-el ./examples/go-xdp-counter/bpf_mips64le_bpfel.o

      --bc-ppc64-eb <BC_PPC64_EB>
          Optional: bytecode file to use for building the image assuming ppc64 architecture.
          Example: --bc-ppc64-eb ./examples/go-xdp-counter/bpf_ppc64_bpfeb.o

      --bc-ppc64le-el <BC_PPC64LE_EL>
          Optional: bytecode file to use for building the image assuming ppc64le architecture.
          Example: --bc-ppc64le-el ./examples/go-xdp-counter/bpf_ppc64le_bpfel.o

      --bc-riscv64-el <BC_RISCV64_EL>
          Optional: bytecode file to use for building the image assuming riscv64 architecture.
          Example: --bc-riscv64-el ./examples/go-xdp-counter/bpf_riscv64_bpfel.o

      --bc-s390x-eb <BC_S390X_EB>
          Optional: bytecode file to use for building the image assuming s390x architecture.
          Example: --bc-s390x-eb ./examples/go-xdp-counter/bpf_s390x_bpfeb.o

  -h, --help
          Print help (see a summary with '-h')
```

Below are some different examples of building images.
Note that `sudo` is not required.
This command also pushed the image to a registry, so user must already be logged into the registry.

Example of single bytecode image:

```console
bpfman image build -f Containerfile.bytecode -t quay.io/$QUAY_USER/go-xdp-counter:test -b ./examples/go-xdp-counter/bpf_x86_bpfel.o
```

Example of directory with Cilium generated bytecode objects:

```console
bpfman image build -f Containerfile.bytecode.multi.arch -t quay.io/$QUAY_USER/go-xdp-counter:test -c ./examples/go-xdp-counter/
```

!!! Note
    To build images for multiple architectures on a local system, docker (or podman) may need additional configuration
    settings to allow for caching of non-native images. See
    [https://docs.docker.com/build/building/multi-platform/](https://docs.docker.com/build/building/multi-platform/)
    for more details.

### bpfman image generate-build-args

The `bpfman image generate-build-args` command is a utility command that generates the labels used
to package eBPF program bytecode in a OCI container image.
It is recommended to use the `bpfman image build` command to package the eBPF program in a OCI
container image, but an alternative is to generate the labels then build the container image with
`docker` or `podman`.

The eBPF program bytecode must already be generated.
The bytecode can take several forms, but at least one must be provided:

* `--bytecode` or `-b`: Use this option for a single bytecode object file built for the host architecture.
  The value of this parameter is a single bytecode object file.
* `--cilium-ebpf-project` or `-c`: Use this option for a cilium/ebpf based project.
  The value of this parameter is a directory that contains multiple object files for different architectures,
  where the object files follow the Cilium naming convention with the architecture in the name (i.e. bpf_x86_bpfel.o,
  bpf_arm64_bpfel.o, bpf_powerpc_bpfel.o, bpf_s390_bpfeb.o).
* `--bc-386-el` .. `--bc-s390x-eb`: Use this option to add one or more architecture specific bytecode files.

```console
bpfman image generate-build-args --help
Generate the OCI image labels for a given bytecode file.

To use, the --container-file and --tag must be included, as well as a pointer to
at least one bytecode file that can be passed in several ways. Use either:

* --bytecode: for a single bytecode built for the host architecture.

* --cilium-ebpf-project: for a cilium/ebpf project directory which contains
    multiple object files for different architectures.

* --bc-386-el .. --bc-s390x-eb: to add one or more architecture specific bytecode files.

Examples:
  bpfman image generate-build-args --bc-amd64-el ./examples/go-xdp-counter/bpf_x86_bpfel.o

Usage: bpfman image generate-build-args <--bytecode <BYTECODE>|--cilium-ebpf-project <CILIUM_EBPF_PROJECT>|--bc-386-el <BC_386_EL>|--bc-amd64-el <BC_AMD64_EL>|--bc-arm-el <BC_ARM_EL>|--bc-arm64-el <BC_ARM64_EL>|--bc-loong64-el <BC_LOONG64_EL>|--bc-mips-eb <BC_MIPS_EB>|--bc-mipsle-el <BC_MIPSLE_EL>|--bc-mips64-eb <BC_MIPS64_EB>|--bc-mips64le-el <BC_MIPS64LE_EL>|--bc-ppc64-eb <BC_PPC64_EB>|--bc-ppc64le-el <BC_PPC64LE_EL>|--bc-riscv64-el <BC_RISCV64_EL>|--bc-s390x-eb <BC_S390X_EB>>

Options:
  -b, --bytecode <BYTECODE>
          Optional: bytecode file to use for building the image assuming host architecture.
          Example: -b ./examples/go-xdp-counter/bpf_x86_bpfel.o

  -c, --cilium-ebpf-project <CILIUM_EBPF_PROJECT>
          Optional: If specified pull multi-arch bytecode files from a cilium/ebpf formatted project
          where the bytecode files all contain a standard bpf_<GOARCH>_<(el/eb)>.o tag.
          Example: --cilium-ebpf-project ./examples/go-xdp-counter

      --bc-386-el <BC_386_EL>
          Optional: bytecode file to use for building the image assuming amd64 architecture.
          Example: --bc-386-el ./examples/go-xdp-counter/bpf_386_bpfel.o

      --bc-amd64-el <BC_AMD64_EL>
          Optional: bytecode file to use for building the image assuming amd64 architecture.
          Example: --bc-amd64-el ./examples/go-xdp-counter/bpf_x86_bpfel.o

      --bc-arm-el <BC_ARM_EL>
          Optional: bytecode file to use for building the image assuming arm architecture.
          Example: --bc-arm-el ./examples/go-xdp-counter/bpf_arm_bpfel.o

      --bc-arm64-el <BC_ARM64_EL>
          Optional: bytecode file to use for building the image assuming arm64 architecture.
          Example: --bc-arm64-el ./examples/go-xdp-counter/bpf_arm64_bpfel.o

      --bc-loong64-el <BC_LOONG64_EL>
          Optional: bytecode file to use for building the image assuming loong64 architecture.
          Example: --bc-loong64-el ./examples/go-xdp-counter/bpf_loong64_bpfel.o

      --bc-mips-eb <BC_MIPS_EB>
          Optional: bytecode file to use for building the image assuming mips architecture.
          Example: --bc-mips-eb ./examples/go-xdp-counter/bpf_mips_bpfeb.o

      --bc-mipsle-el <BC_MIPSLE_EL>
          Optional: bytecode file to use for building the image assuming mipsle architecture.
          Example: --bc-mipsle-el ./examples/go-xdp-counter/bpf_mipsle_bpfel.o

      --bc-mips64-eb <BC_MIPS64_EB>
          Optional: bytecode file to use for building the image assuming mips64 architecture.
          Example: --bc-mips64-eb ./examples/go-xdp-counter/bpf_mips64_bpfeb.o

      --bc-mips64le-el <BC_MIPS64LE_EL>
          Optional: bytecode file to use for building the image assuming mips64le architecture.
          Example: --bc-mips64le-el ./examples/go-xdp-counter/bpf_mips64le_bpfel.o

      --bc-ppc64-eb <BC_PPC64_EB>
          Optional: bytecode file to use for building the image assuming ppc64 architecture.
          Example: --bc-ppc64-eb ./examples/go-xdp-counter/bpf_ppc64_bpfeb.o

      --bc-ppc64le-el <BC_PPC64LE_EL>
          Optional: bytecode file to use for building the image assuming ppc64le architecture.
          Example: --bc-ppc64le-el ./examples/go-xdp-counter/bpf_ppc64le_bpfel.o

      --bc-riscv64-el <BC_RISCV64_EL>
          Optional: bytecode file to use for building the image assuming riscv64 architecture.
          Example: --bc-riscv64-el ./examples/go-xdp-counter/bpf_riscv64_bpfel.o

      --bc-s390x-eb <BC_S390X_EB>
          Optional: bytecode file to use for building the image assuming s390x architecture.
          Example: --bc-s390x-eb ./examples/go-xdp-counter/bpf_s390x_bpfeb.o

  -h, --help
          Print help (see a summary with '-h')
```

Below are some different examples of generating build arguments.
Note that `sudo` is not required.

Example of single bytecode image:

```console
$ bpfman image generate-build-args -b ./examples/go-xdp-counter/bpf_x86_bpfel.o
BYTECODE_FILE=./examples/go-xdp-counter/bpf_x86_bpfel.o
PROGRAMS={"xdp_stats":"xdp"}
MAPS={"xdp_stats_map":"per_cpu_array"}
```

Example of directory with Cilium generated bytecode objects:

```console
$ bpfman image generate-build-args -c ./examples/go-xdp-counter/
BC_AMD64_EL=./examples/go-xdp-counter/bpf_x86_bpfel.o
BC_ARM_EL=./examples/go-xdp-counter/bpf_arm64_bpfel.o
BC_PPC64LE_EL=./examples/go-xdp-counter/bpf_powerpc_bpfel.o
BC_S390X_EB=./examples/go-xdp-counter/bpf_s390_bpfeb.o
PROGRAMS={"xdp_stats":"xdp"}
MAPS={"xdp_stats_map":"per_cpu_array"}
```

Once the labels are generated, the eBPF program can be packaged in a OCI
container image using `docker` or `podman` by passing the generated labels
as `build-arg` parameters:

```console
docker build \
  --build-arg BYTECODE_FILE=./examples/go-xdp-counter/bpf_x86_bpfel.o \
  --build-arg PROGRAMS={"xdp_stats":"xdp"} \
  --build-arg MAPS={"xdp_stats_map":"per_cpu_array"} \
  -f Containerfile.bytecode . -t quay.io/$USER/go-xdp-counter-bytecode:test
```

## Container Runtime Integration

bpfman integrates with your local container runtime (Docker or Podman) to simplify the workflow for loading eBPF programs, especially during development.

### Using Container Runtime Images

When loading programs, bpfman can now use images directly from your container runtime's local storage:

```console
# Build an eBPF program container image locally
docker build -t myebpf:latest ./my-ebpf-program/

# Load the program directly from the local container runtime storage
sudo bpfman load image --image-url myebpf:latest --name "my_program" xdp --iface eth0
```

### Configuration

To enable container runtime integration, add the following configuration to your `bpfman` settings:

```ini
[container_runtime]
enabled = true                 # Set to false to disable container runtime integration
preferred_runtime = "docker"   # Optional: Specify preferred runtime if multiple are available
```
