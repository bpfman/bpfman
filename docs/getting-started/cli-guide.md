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
sudo bpfman --help
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
          Example: --path /run/bpfman/examples/go-xdp-counter/bpf_bpfel.o

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

Usage: bpfman load image [OPTIONS] --image-url <IMAGE_URL> <COMMAND>

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
          Optional: The name of the function that is the entry point for the BPF program.
          If not provided, the program name defined as part of the bytecode image will be used.

          [default: ]

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
sudo bpfman load file --path $HOME/src/bpfman/tests/integration-test/bpf/.output/xdp_pass.bpf.o --name "pass" xdp --iface vethb2795c7 --priority 100
```

Example from image in remote repository (Note: `--name` is built into the image and is not required):

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 100
```

The `tc` command is similar to `xdp`, but it also requires the `direction` option
and the `proceed-on` values are different.

```console
sudo bpfman load file tc -h
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
sudo bpfman load file -p $HOME/src/bpfman/tests/integration-test/bpf/.output/tc_pass.bpf.o -n "pass" tc -d ingress -i mynet1 -p 40
```

For the `tc_pass.bpf.o` program loaded with the command above, the name
would be set as shown in the following snippet:

```c
SEC("classifier/pass")
int accept(struct __sk_buff *skb)
{
    :
}
```

### Additional Load Examples

Below are some additional examples of `bpfman load` commands:

#### Fentry

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/fentry:latest fentry -f do_unlinkat
```

#### Fexit

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/fexit:latest fexit -f do_unlinkat
```

#### Kprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/kprobe:latest kprobe -f try_to_wake_up
```

#### Kretprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/kretprobe:latest kprobe -f try_to_wake_up -r
```

#### TC

```console
sudo bpfman load file --path $HOME/src/bpfman/examples/go-tc-counter/bpf_bpfel.o --name "stats"" tc --direction ingress --iface vethb2795c7 --priority 110
```

#### Uprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/uprobe:latest uprobe -f "malloc" -t "libc"
```

#### Uretprobe

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/uretprobe:latest uprobe -f "malloc" -t "libc" -r
```

#### XDP

```console
sudo bpfman load file --path $HOME/src/bpfman/examples/go-xdp-counter/bpf_bpfel.o --name "xdp_stats" xdp --iface vethb2795c7 --priority 35
```

### Setting Global Variables in eBPF Programs

Global variables can be set for any eBPF program type when loading as follows:

```console
sudo bpfman load file -p $HOME/src/bpfman/tests/integration-test/bpf/.output/tc_pass.bpf.o -g GLOBAL_u8=01020304 GLOBAL_u32=0A0B0C0D -n "pass" tc -d ingress -i mynet1 -p 40
```

Note, that when setting global variables, the eBPF program being loaded must
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
sudo bpfman load file -p $HOME/src/bpfman/tests/integration-test/bpf/.output/xdp_pass.bpf.o -n "pass" xdp -i mynet1 -p 30 --proceed-on drop pass dispatcher_return
```

### Sharing Maps Between eBPF Programs

> **WARNING** Currently for the map sharing feature to work the LIBBPF_PIN_BY_NAME
flag **MUST** be set in the shared bpf map definitions. Please see [this aya issue](https://github.com/aya-rs/aya/issues/837) for future work that will change this requirement.

To share maps between eBPF programs, first load the eBPF program that owns the
maps.
One eBPF program must own the maps.

```console
sudo bpfman load file --path $HOME/src/bpfman/examples/go-xdp-counter/bpf_bpfel.o -n "xdp_stats" xdp --iface vethb2795c7 --priority 100
6371
```

Next, load additional eBPF programs that will share the existing maps by passing
the program id of the eBPF program that owns the maps using the `--map-owner-id`
parameter:

```console
sudo bpfman load file --path $HOME/src/bpfman/examples/go-xdp-counter/bpf_bpfel.o -n "xdp_stats" --map-owner-id 6371 xdp --iface vethff657c7 --priority 100
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
 Path:          /home/<$USER>/src/bpfman/examples/go-xdp-counter/bpf_bpfel.o
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6371
 Map Owner ID:  None
 Map Used By:   6371
                6373
 Priority:      50
 Iface:         vethff657c7
 Position:      1
 Proceed On:    pass, dispatcher_return
:
```

```console
sudo bpfman get 6373
 Bpfman State
---------------
 Name:          xdp_stats
 Path:          /home/<$USER>/src/bpfman/examples/go-xdp-counter/bpf_bpfel.o
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/6371
 Map Owner ID:  6371
 Map Used By:   6371
                6373
 Priority:      50
 Iface:         vethff657c7
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
 Iface:         vethff657c7
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
Size JITed (bytes):                55
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

## bpfman image pull

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
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --pull-policy IfNotPresent xdp --iface vethff657c7 --priority 100
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
 Iface:         vethff657c7                           
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
