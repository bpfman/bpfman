# bpfctl Guide

`bpfctl` is the command line tool for interacting with `bpfd`.
`bpfctl` allows the user to `load`, `unload` and `list` BPF programs.

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
This includes BPF object files.
In the `bpfctl load-from-file` examples below, the files are located in `/run/bpfd/examples/`, which is
a directory owned by `bpfd`.
Below is an example of copying local files over for use in this scenario:

```console
sudo cp /$HOME/src/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o /run/bpfd/examples/.
sudo cp /$HOME/src/net-ebpf-playground/.output/filter.bpf.o /run/bpfd/examples/.
sudo chown bpfd:bpfd -R /run/bpfd/examples/
```

This is only needed if `bpfd` is run as a systemd service.

## Basic Syntax

Below are the commands supported by `bpfctl`.

```console
bpfctl --help
A client for working with bpfd

Usage: bpfctl <COMMAND>

Commands:
  load-from-file   Load a BPF program from a local .o file
  load-from-image  Load a BPF program packaged in a OCI container image from a given registry
  unload           Unload a BPF program using the UUID
  list             List all BPF programs loaded via bpfd
  help             Print this message or the help of the given subcommand(s)

Options:
  -h, --help     Print help information
  -V, --version  Print version information
```

## bpfctl load

The `bpfctl load-from-file` and `bpfctl load-from-image` commands are used to load BPF programs.
Each program type (i.e. `<COMMAND>`) has it's own set of attributes specific to the program type,
and those attributes MUST come after the program type is entered.
There are a common set of attributes, and those MUST come before the program type is entered.

```console
bpfctl load-from-file --help
Load a BPF program from a local .o file

Usage: bpfctl load-from-file [OPTIONS] --path <PATH> --section-name <SECTION_NAME> <COMMAND>

Commands:
  xdp
          Install an eBPF program on an XDP hook point for a given interface
  tc
          Install an eBPF program on a TC hook point for a given interface
  tracepoint
          Install an eBPF program on a Tracepoint
  help
          Print this message or the help of the given subcommand(s)

Options:
  -p, --path <PATH>
          Required: Location of Local bytecode file
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

  -h, --help
          Print help (see a summary with '-h')
```

So when using `bpfctl load-from-file`, `--path`, `--section-name`, `--id` and `--global` must
be entered before the `<COMMAND>` (`xdp`, `tc` or `tracepoint`) is entered.
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

### bpfctl Load Examples

Below are some examples of `bpfctl load` commands:

```console
bpfctl load-from-file --path /run/bpfd/examples/xdp_pass_kern.o --section-name "xdp" xdp --iface vethb2795c7 --priority 35


bpfctl load-from-file --path /run/bpfd/examples/filter.bpf.o --section-name classifier tc --direction ingress --iface vethb2795c7 --priority 110


bpfctl load-from-image --image-url quay.io/bpfd-bytecode/tracepoint:latest tracepoint --tracepoint sched/sched_switch
```

## bpfctl list

The `bpfctl list` command lists all the loaded BPF programs:

```console
bpfctl list
 UUID                                  Type        Name        Location                                                                           Metadata
 9d37c6c7-d988-41da-ac89-200655f61584  xdp         xdp         file: { path: /run/bpfd/examples/xdp_pass_kern.o }                                 { priority: 35, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }
 c1e8691e-bfd7-48a7-bdeb-e2b429bfc2f4  tracepoint  hello       image: { url: quay.io/bpfd-bytecode/tracepoint:latest, pullpolicy: IfNotPresent }  { tracepoint: sched/sched_switch }
 84eff4d7-6dbb-4ed7-9ce4-d6b5478e8d91  tc          classifier  file: { path: /run/bpfd/examples/filter.bpf.o }                                    { priority: 110, iface: vethb2795c7, position: 0, direction: in, proceed_on: pipe, dispatcher_return }
```

## bpfctl unload

The `bpfctl unload` command takes the UUID from the load or list command as a parameter,
and unloads the requested BPF program:

```console
bpfctl unload 84eff4d7-6dbb-4ed7-9ce4-d6b5478e8d91


bpfctl list
 UUID                                  Type        Name        Location                                                                           Metadata
 9d37c6c7-d988-41da-ac89-200655f61584  xdp         xdp         file: { path: /run/bpfd/examples/xdp_pass_kern.o }                                 { priority: 35, iface: vethb2795c7, position: 0, proceed_on: pass, dispatcher_return }
 c1e8691e-bfd7-48a7-bdeb-e2b429bfc2f4  tracepoint  hello       image: { url: quay.io/bpfd-bytecode/tracepoint:latest, pullpolicy: IfNotPresent }  { tracepoint: sched/sched_switch }
dispatcher_return }
```
