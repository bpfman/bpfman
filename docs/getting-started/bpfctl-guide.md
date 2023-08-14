# bpfctl Guide

`bpfctl` is the command line tool for interacting with `bpfd`.
`bpfctl` allows the user to `load`, `unload` and `list` eBPF programs.

## Notes For This Guide

As described in other sections, `bpfd` can be run as either a privileged process or
a systemd service.
If run as a privileged process, `bpfctl` will most likely be run from your local
development branch and will require `sudo`.
Example:

```console
sudo ./target/debug/bpfctl list
```

If run as a systemd service, `bpfctl` will most likely be installed in your $PATH,
the `bpfd` user and user group were created, so the usergroup `bpfd` will need to be
added to the desired user.
Then `sudo` is no longer required.
Example:

```console
sudo usermod -a -G bpfd $USER
exit
<LOGIN>

bpfctl list
```

The examples here use `bpfctl` in place of `sudo ./target/debug/bpfctl` for readability,
use as your system is deployed.

### bpfctl load-from-file With bpfd As A Systemd Service

For security reasons, when `bpfd` is run as a systemd service, all linux capabilities are stripped
from any spawned threads.
Therefore, `bpfd` can only access files owned by the `bpfd` user group.
This includes eBPF object files.
In the `bpfctl load-from-file` examples below, the files are located in `/run/bpfd/examples/`, which is
a directory owned by `bpfd`.
Below is an example of copying local files over for use in this scenario:

```console
sudo cp /$HOME/src/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o /run/bpfd/examples/.
sudo cp /$HOME/src/net-ebpf-playground/.output/filter.bpf.o /run/bpfd/examples/.
sudo chown bpfd:bpfd -R /run/bpfd/examples/
```

This is only needed if `bpfd` is run as a systemd service.
When `sudo ./scripts/setup.sh install` is run to launch bpfd as a systemd service, all the
eBPF object files from the `examples/` directory are copied to `/run/bpfd/examples/`.

## Basic Syntax

Below are the commands supported by `bpfctl`.

```console
bpfctl --help
A client for working with bpfd

Usage: bpfctl <COMMAND>

Commands:
  load-from-file   Load an eBPF program from a local .o file
  load-from-image  Load an eBPF program packaged in a OCI container image from a given registry
  unload           Unload an eBPF program using the UUID
  list             List all eBPF programs loaded via bpfd
  get              Get a program's metadata by kernel id
  help             Print this message or the help of the given subcommand(s)

Options:
  -h, --help     Print help information
  -V, --version  Print version information
```

## bpfctl load

The `bpfctl load-from-file` and `bpfctl load-from-image` commands are used to load eBPF programs.
Each program type (i.e. `<COMMAND>`) has it's own set of attributes specific to the program type,
and those attributes MUST come after the program type is entered.
There are a common set of attributes, and those MUST come before the program type is entered.

```console
bpfctl load-from-file --help
Load an eBPF program from a local .o file

Usage: bpfctl load-from-file [OPTIONS] --path <PATH> --section-name <SECTION_NAME> <COMMAND>

Commands:
  xdp         Install an eBPF program on the XDP hook point for a given interface
  tc          Install an eBPF program on the TC hook point for a given interface
  tracepoint  Install an eBPF program on a Tracepoint
  uprobe      Install an eBPF uprobe
  help        Print this message or the help of the given subcommand(s)

Options:
  -p, --path <PATH>
          Required: Location of local bytecode file
          Example: --path /run/bpfd/examples/go-xdp-counter/bpf_bpfel.o

  -s, --section-name <SECTION_NAME>
          Required: Name of the ELF section from the object file

      --id <ID>
          Optional: Program uuid to be used by bpfd. If not specified, bpfd will generate
          a uuid.

  -g, --global <GLOBAL>...
          Optional: Global variables to be set when program is loaded.
          Format: <NAME>=<Hex Value>
          
          This is a very low level primitive. The caller is responsible for formatting
          the byte string appropriately considering such things as size, endianness,
          alignment and packing of data structures.

      --map-owner-uuid <map_owner_id>
          Optional: UUID of loaded eBPF program this eBPF program will share a map with.
          Only used when multiple eBPF programs need to share a map. If a map is being
          shared with another eBPF program, the eBPF program that created the map can not
          be unloaded until all eBPF programs referencing the map are unloaded.
          Example: --map-owner-uuid 989958a5-b47b-47a5-8b4c-b5962292437d

  -h, --help
          Print help (see a summary with '-h')
```

So when using `bpfctl load-from-file`, `--path`, `--section-name`, `--id`, `--global`
and `--map-owner-uuid` must be entered before the `<COMMAND>` (`xdp`, `tc` or `tracepoint`)
is entered.
Then each `<COMMAND>` has it's own custom parameters:

```console
bpfctl load-from-file xdp --help
Install an eBPF program on an XDP hook point for a given interface

Usage: bpfctl load-from-file --path <PATH> --section-name <SECTION_NAME> xdp [OPTIONS] --iface <IFACE> --priority <PRIORITY>

Options:
  -i, --iface <IFACE>               Required: Interface to load program on
  -p, --priority <PRIORITY>         Required: Priority to run program in chain. Lower value runs first
      --proceed-on <PROCEED_ON>...  Optional: Proceed to call other programs in chain on this exit code.
                                    Multiple values supported by repeating the parameter.
                                    Valid values: [aborted, drop, pass, tx, redirect, dispatcher_return]
                                    Example: --proceed-on "pass" --proceed-on "drop"
                                    [default: pass, dispatcher_return]
  -h, --help                        Print help
```

Example loading from local file:

```console
bpfctl load-from-file --path /run/bpfd/examples/xdp_pass_kern.o --section-name "xdp" xdp --iface vethb2795c7 --priority 100
```

Example from image in remote repository (Note: `--section-name` is built into the image and is not required):

```console
bpfctl load-from-image --image-url quay.io/bpfd-bytecode/xdp_pass:latest xdp --iface vethb2795c7 --priority 100
```

The `tc` command is similar to `xdp`, but it also requires the `direction` option
and the `proceed-on` values are different.

```console
bpfctl load-from-file tc -h
Install an eBPF program on the TC hook point for a given interface

Usage: bpfctl load-from-file --path <PATH> --section-name <SECTION_NAME> tc [OPTIONS] --direction <DIRECTION> --iface <IFACE> --priority <PRIORITY>

Options:
  -d, --direction <DIRECTION>       Required: Direction to apply program. Valid values: [ingress, egress]
  -i, --iface <IFACE>               Required: Interface to load program on
  -p, --priority <PRIORITY>         Required: Priority to run program in chain. Lower value runs first
      --proceed-on <PROCEED_ON>...  Optional: Proceed to call other programs in chain on this exit code.
                                    Multiple values supported by repeating the parameter.
                                    Valid values: [unspec, ok, reclassify, shot, pipe, stolen, queued,
                                    repeat, redirect, trap, dispatcher_return]
                                    Example: --proceed-on "ok" --proceed-on "pipe"
                                    [default: ok, pipe, dispatcher_return]
  -h, --help                        Print help
```

The following is an example of the `tc` command using short option names:

```console
bpfctl load-from-file -p /run/bpfd/examples/accept-all.o -s "accept" tc -d ingress -i mynet1 -p 40
```

For the `accept-all.o` program loaded with the command above, the section name
would be set as shown in the following snippet:

```c
SEC("classifier/accept")
int accept(struct __sk_buff *skb)
{
```

### Additional bpfctl Load Examples

Below are some additional examples of `bpfctl load` commands:

```console
bpfctl load-from-file --path /run/bpfd/examples/xdp_pass_kern.o --section-name "xdp" xdp --iface vethb2795c7 --priority 35

bpfctl load-from-file --path /run/bpfd/examples/filter.bpf.o --section-name classifier tc --direction ingress --iface vethb2795c7 --priority 110

bpfctl load-from-image --image-url quay.io/bpfd-bytecode/tracepoint:latest tracepoint --tracepoint sched/sched_switch
```

### Setting Global Variables in eBPF Programs

Global variables can be set for any eBPF program type when loading as follows:

```console
bpfctl load-from-file -p /run/bpfd/examples/accept-all.o -g GLOBAL_1=01020304 GLOBAL_2=0A0B0C0D -s "accept" tc -d ingress -i mynet1 -p 40
```

Note, that when setting global variables, the eBPF program being loaded must
have global variables named with the strings given, and the size of the value
provided must match the size of the given variable.  For example, the above
command can be used to update the following global variables in an eBPF program.

```c
volatile const __u32 GLOBAL_1 = 0;
volatile const __u32 GLOBAL_2 = 0;
```

### Modifying the Proceed-On Behavior

The `proceed-on` setting applies to `xdp` and `tc` programs. For both of these
program types, an ordered list of eBPF programs is maintained per attach point.
The `proceed-on` setting determines whether processing will "proceed" to the
next eBPF program in the list, or terminate processing and return, based on the
program's return value. For example, the default `proceed-on` configuration for
an `xdp` program can be modified as follows:

```console
bpfctl load-from-file -p /run/bpfd/examples/xdp_pass_kern.o -s "xdp" xdp -i mynet1 -p 30 --proceed-on drop pass dispatcher_return
```

### Sharing Maps Between eBPF Programs

To share maps between eBPF programs, first load the eBPF program that owns the
maps.
One eBPF program must own the maps.

```console
bpfctl load-from-file --path /run/bpfd/examples/go-xdp-counter/bpf_bpfel.o -s "stats" xdp --iface vethb2795c7 --priority 100
87100e16-4481-4f97-be89-f68d269d6062
```

Next, load additional eBPF programs that will share the existing maps by passing
the UUID of the eBPF program that owns the maps using the `--map-owner-uuid`
parameter:

```console
bpfctl load-from-file --path /run/bpfd/examples/go-xdp-counter/bpf_bpfel.o -s "stats" --map-owner-uuid 87100e16-4481-4f97-be89-f68d269d6062 xdp --iface vethff657c7 --priority 100
d6939812-5f6a-42ff-9b55-d3668d8527d0
```

Use the `bpfctl get <Kernel_ID>` command to display the configuration:

```console
bpfctl list
 Kernel ID  Bpfd UUID                             Name   Type  Load Time                
 6371       87100e16-4481-4f97-be89-f68d269d6062  stats  xdp   2023-07-18T16:50:46-0400 
 6373       d6939812-5f6a-42ff-9b55-d3668d8527d0  stats  xdp   2023-07-18T16:51:06-0400 

bpfctl get 6371

#################### Bpfd State ####################

UUID:                               87100e16-4481-4f97-be89-f68d269d6062
Path:                               /run/bpfd/examples/go-xdp-counter/bpf_bpfel.o
Global:                             None
Map Pin Path:                       /run/bpfd/fs/maps/87100e16-4481-4f97-be89-f68d269d6062
Map Owner UUID:                     None
Map Used By:                        d6939812-5f6a-42ff-9b55-d3668d8527d0
Priority:                           50
Iface:                              vethff657c7
Position:                           1
Proceed On:                         pass, dispatcher_return
:

bpfctl get 6373

#################### Bpfd State ####################

UUID:                               d6939812-5f6a-42ff-9b55-d3668d8527d0
Path:                               /run/bpfd/examples/go-xdp-counter/bpf_bpfel.o
Global:                             None
Map Pin Path:                       /run/bpfd/fs/maps/87100e16-4481-4f97-be89-f68d269d6062
Map Owner UUID:                     87100e16-4481-4f97-be89-f68d269d6062
Map Used By:                        d6939812-5f6a-42ff-9b55-d3668d8527d0
Priority:                           50
Iface:                              vethff657c7
Position:                           0
Proceed On:                         pass, dispatcher_return
:
```

As the output shows, the first program (`87100e16-4481-4f97-be89-f68d269d6062`)
owns the map, with `Map Program UUID` blank and the `Map Pin Path`
(`/run/bpfd/fs/maps/87100e16-4481-4f97-be89-f68d269d6062`) that includes its own
UUID.

The second program (`d6939812-5f6a-42ff-9b55-d3668d8527d0`) references the first
program via the `Map Program UUID` set to `87100e16-4481-4f97-be89-f68d269d6062`
and the `Map Pin Path` (`/run/bpfd/fs/maps/87100e16-4481-4f97-be89-f68d269d6062`)
set to same directory as the first program, which includes the first program's UUID.
The output for both commands shows the map is being used by the second program via
the `Map Used By` with a value of `d6939812-5f6a-42ff-9b55-d3668d8527d0`.

The eBPF programs can be unloaded any order, the `Map Pin Path` will not be deleted
until all the programs referencing the maps are unloaded:

```console
bpfctl unload d6939812-5f6a-42ff-9b55-d3668d8527d0
bpfctl unload 87100e16-4481-4f97-be89-f68d269d6062
```

## bpfctl list

The `bpfctl list` command lists all the bpfd loaded eBPF programs:

```console
bpfctl list
 Kernel ID  Bpfd UUID                             Name              Type        Load Time
 6201       96c4671c-e764-4016-8e79-ee99b2d58c12  pass              xdp         2023-07-17T17:17:53-0400
 6202       995e87fe-4d1d-48ce-b348-3411342cf661  sys_enter_openat  tracepoint  2023-07-17T17:19:09-0400
 6204       665954eb-0532-4849-8db6-e127e3fe3072  stats             tc          2023-07-17T17:20:14-0400
```

To see all eBPF programs loaded on the system, include the `--all` option.
eBPF Programs loaded via bpfd will have a `Bpfd UUID` and a `Kernel ID`.
eBPF Programs loaded outside of bpfd will only have a `Kernel ID`.


```console
bpfctl list --all
 Kernel ID  Bpfd UUID                             Name              Type           Load Time
 52                                               restrict_filesy   lsm            2023-05-03T12:53:34-0400
 166                                              dump_bpf_map      tracing        2023-05-03T12:53:52-0400
 167                                              dump_bpf_prog     tracing        2023-05-03T12:53:52-0400
 455                                                                cgroup_device  2023-05-03T12:58:26-0400
 :
 6190                                                               cgroup_skb     2023-07-17T17:15:23-0400
 6191                                                               cgroup_device  2023-07-17T17:15:23-0400
 6192                                                               cgroup_skb     2023-07-17T17:15:23-0400
 6193                                                               cgroup_skb     2023-07-17T17:15:23-0400
 6194                                                               cgroup_device  2023-07-17T17:15:23-0400
 6201       96c4671c-e764-4016-8e79-ee99b2d58c12  pass              xdp            2023-07-17T17:17:53-0400
 6202       995e87fe-4d1d-48ce-b348-3411342cf661  sys_enter_openat  tracepoint     2023-07-17T17:19:09-0400
 6203                                             dispatcher        tc             2023-07-17T17:20:14-0400
 6204       665954eb-0532-4849-8db6-e127e3fe3072  stats             tc             2023-07-17T17:20:14-0400
 6207                                             xdp               xdp            2023-07-17T17:27:13-0400
```

To filter on a given program type, include the `--program-type` parameter:

```console
bpfctl list --all --program-type tc
 Kernel ID  Bpfd UUID                             Name        Type  Load Time
 6203                                             dispatcher  tc    2023-07-17T17:20:14-0400
 6204       665954eb-0532-4849-8db6-e127e3fe3072  stats       tc    2023-07-17T17:20:14-0400
```

## bpfctl get

To retrieve detailed information for a loaded eBPF program, use the
`bpfctl get <Kernel_ID>` command.
If the eBPF program was loaded via bpfd, then there will be a `Bpfd State`
section with bpfd related attributes and a `Kernel State` section with
kernel information.
If the eBPF program was loaded outside of bpfd, then the `Bpfd State`
section will be empty and `Kernel State` section will be populated.

```console
bpfctl get 6204

#################### Bpfd State ####################

UUID:                               665954eb-0532-4849-8db6-e127e3fe3072
Image URL:                          quay.io/bpfd-bytecode/go-tc-counter:latest
Pull Policy:                        IfNotPresent
Global:                             None
Map Pin Path:                       /run/bpfd/fs/maps/665954eb-0532-4849-8db6-e127e3fe3072
Map Owner UUID:                     None
Map Used By:                        None
Priority:                           100
Iface:                              vethff657c7
Position:                           0
Direction:                          eg
Proceed On:                         pipe, dispatcher_return

#################### Kernel State ##################

Kernel ID:                          6204
Name:                               stats
Type:                               tc
Loaded At:                          2023-07-17T17:20:14-0400
Tag:                                ead94553702a3742
GPL Compatible:                     true
Map IDs:                            [2705]
BTF ID:                             2821
Size Translated (bytes):            176
JITed:                              true
Size JITed (bytes):                 116
Kernel Allocated Memory (bytes):    4096
Verified Instruction Count:         24
```

```console
bpfctl get 6190

#################### Bpfd State ####################
NONE

#################### Kernel State ##################

Kernel ID:                          6190
Name:                               None
Type:                               cgroup_skb
Loaded At:                          2023-07-17T17:15:23-0400
Tag:                                6deef7357e7b4530
GPL Compatible:                     true
Map IDs:                            []
BTF ID:                             0
Size Translated (bytes):            64
JITed:                              true
Size JITed (bytes):                 55
Kernel Allocated Memory (bytes):    4096
Verified Instruction Count:         8
```

## bpfctl unload

The `bpfctl unload` command takes the UUID from the load or list command as a parameter,
and unloads the requested eBPF program:

```console
bpfctl unload 665954eb-0532-4849-8db6-e127e3fe3072


bpfctl list
 Kernel ID  Bpfd UUID                             Name              Type        Load Time
 6201       96c4671c-e764-4016-8e79-ee99b2d58c12  pass              xdp         2023-07-17T17:17:53-0400
 6202       995e87fe-4d1d-48ce-b348-3411342cf661  sys_enter_openat  tracepoint  2023-07-17T17:19:09-0400
```
