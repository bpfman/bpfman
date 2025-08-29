# CLI Guide

`bpfman` offers several CLI commands to manage eBPF programs.
The CLI allows you to `load`, `attach`, `detach`, `unload`, `get` and `list` eBPF programs.

## Notes For This Guide

As described in other sections, `bpfman` can be run as either a privileged process or
a systemd service.
If run as a privileged process, `bpfman` will most likely be run from your local
development branch and will require `sudo`.
Example:

```console
sudo ./target/debug/bpfman list programs
```

If run as a systemd service, `bpfman` will most likely be installed in your $PATH,
and will also require `sudo`.
Example:

```console
sudo bpfman list programs
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
$ bpfman --help
An eBPF manager focusing on simplifying the deployment and administration of eBPF programs.

Usage: bpfman <COMMAND>

Commands:
  load    Load an eBPF program on the system
  unload  Unload an eBPF program using the Program Id
  attach  Attach an eBPF program to a hook point using the Program Id
  detach  Detach an eBPF program from a hook point using the Link Id
  list    List all loaded eBPF programs or attached links
  get     Get a loaded eBPF program or program attachment link
  image   eBPF Bytecode Image related commands
  help    Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')
```

The general flow for using the CLI is as follows:

* **Load Program**: The first step is to load the eBPF Program (or Programs) using the
  `bpfman load` command.
  The programs can be from a locally built eBPF program (.o file) or an eBPF program
  packaged in an OCI container image from a given registry.
  Once the command completes successfully, the eBPF programs are loaded in kernel memory,
  but have not yet been attached to any hook points.
* **Attach Program**: The next step is to attach a loaded eBPF Program to a hook point
  using the `bpfman attach` command.
  Each program type (kprobe, tc, tracepoint, xdp, etc) has unique hook points and unique
  configuration data which is provided with the attach command.
  Once attached, the eBPF Program will be called when its hook point is triggered.
* **Display Programs**: At any time, the set of programs loaded and attach can be displayed.
  To get a list of all the programs, use the `bpfman list programs` command.
  If a program shows up in the list then the program has been loaded.
  One of the attributes in the output is  the `Links` parameter.
  If there is a values in the `Links` parameter, then the program has also been attached.
  To retrieve all the parameters of a given program, use the `bpfman get program` command, which
  displays a given program based on its `Program ID`, which can be found in the list output.
  To get a list of all the links, use the `bpfman list links` command.
  To retrieve all the parameters of a given link, use the `bpfman get link` command which
  displays a given link based on its `Link ID` which can be found in the list output.
* **Detach Program**: Optionally, an eBPF Program can be detached from a hook point if desired
  using the `bpfman detach` command.
* **Unload Program**: Once an eBPF Program is no longer needed, the program can be unloaded
  using the `bpfman unload` command.
  The program does not need to be detached before being unloaded.

## bpfman load

The `bpfman load file` and `bpfman load image` commands are used to load eBPF programs.
If the command is successful, the eBPF programs are loaded in kernel memory, but have
not been attached to any hook points yet (see [bpfman attach](#bpfman-attach)).
If the bytecode file contains multiple eBPF programs, they should be loaded in a single
command by passing multiple <TYPE\>:<NAME\> pairs to the `--programs` parameters.
They need to be loaded in a single command so that each of the eBPF programs can share any
global data and maps between them.

The `bpfman load file` command is used to load a locally built eBPF program.
The `bpfman load image` command is used to load an eBPF program packaged in an OCI container
image from a given registry.

```console
$ sudo bpfman load file --help
Load an eBPF program from a local .o file

Usage: bpfman load file [OPTIONS] --programs <PROGRAMS>... --path <PATH>

Options:
      --programs <PROGRAMS>...
          Required: The program type and eBPF function name that is the entry point
          for the eBPF program.
          Format <TYPE>:<FUNC_NAME>

          For fentry and fexit, the function that is being attached to is also
          required at load time, so the format for fentry and fexit includes attach
          function.
          Format <TYPE>:<FUNC_NAME>:<ATTACH_FUNC>

          If the bytecode file contains multiple eBPF programs that need to be
          loaded, multiple eBPF programs can be entered by separating each
          <TYPE>:<FUNC_NAME> pair with a space.
          Example: --programs xdp:xdp_stats kprobe:kprobe_counter
          Example: --programs fentry:test_fentry:do_unlinkat

          [possible values for <TYPE>: fentry, fexit, kprobe, tc, tcx, tracepoint,
                                       uprobe, xdp]

  -p, --path <PATH>
          Required: Location of local bytecode file
          Example: --path /run/bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o

  -g, --global <GLOBAL>...
          Optional: Global variables to be set when program is loaded.
          Format: <NAME>=<Hex Value>

          This is a very low level primitive. The caller is responsible for formatting
          the byte string appropriately considering such things as size, endianness,
          alignment and packing of data structures. Multiple values can be enter by
          separating each <NAME>=<Hex Value> pair with a space.
          Example: -g GLOBAL_u8=01 GLOBAL_u32=0A0B0C0D

  -a, --application <APPLICATION>
          Optional: Application is used to group multiple programs that are loaded together
          under the same load command. This actually creates a special <KEY>=<VALUE> in the
          metadata parameter. It can be used to filer on list commands.
          Example: --application TestEbpfApp

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
$ sudo bpfman load image --help
Load an eBPF program packaged in a OCI container image from a given registry

Usage: bpfman load image [OPTIONS] --programs <PROGRAMS>... --image-url <IMAGE_URL>

Options:
      --programs <PROGRAMS>...
          Required: The program type and eBPF function name that is the entry point
          for the eBPF program.
          Format <TYPE>:<FUNC_NAME>

          For fentry and fexit, the function that is being attached to is also
          required at load time, so the format for fentry and fexit includes attach
          function.
          Format <TYPE>:<FUNC_NAME>:<ATTACH_FUNC>

          If the bytecode file contains multiple eBPF programs that need to be
          loaded, multiple eBPF programs can be entered by separating each
          <TYPE>:<FUNC_NAME> pair with a space.
          Example: --programs xdp:xdp_stats kprobe:kprobe_counter
          Example: --programs fentry:test_fentry:do_unlinkat

          [possible values for <TYPE>: fentry, fexit, kprobe, tc, tcx, tracepoint,
                                       uprobe, xdp]

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

  -g, --global <GLOBAL>...
          Optional: Global variables to be set when program is loaded.
          Format: <NAME>=<Hex Value>

          This is a very low level primitive. The caller is responsible for formatting
          the byte string appropriately considering such things as size, endianness,
          alignment and packing of data structures. Multiple values can be enter by
          separating each <NAME>=<Hex Value> pair with a space.
          Example: -g GLOBAL_u8=01 GLOBAL_u32=0A0B0C0D

  -a, --application <APPLICATION>
          Optional: Application is used to group multiple programs that are loaded together
          under the same load command. This actually creates a special <KEY>=<VALUE> in the
          metadata parameter. It can be used to filer on list commands.
          Example: --application TestEbpfApp

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

### Example Load Commands

Below are some different examples for using the `bpfmam load` command.

#### Loading From Local File

The following is an example of the `tc` command from local file:

```console
$ cd bpfman/
$ sudo bpfman load file -p tests/integration-test/bpf/.output/tc_pass.bpf/bpf_x86_bpfel.o \
     --programs tc:pass --application TcPassProgram
 Bpfman State                                                                  
 BPF Function:  pass                                                           
 Program Type:  tc                                                             
 Path:          tests/integration-test/bpf/.output/tc_pass.bpf/bpf_x86_bpfel.o 
 Global:        None                                                           
 Metadata:      bpfman_application=TcPassProgram                               
 Map Pin Path:  /run/bpfman/fs/maps/90                                         
 Map Owner ID:  None                                                           
 Maps Used By:  90                                                             
 Links:         None                                                           

 Kernel State                                               
 Program ID:                       90                       
 BPF Function:                     pass                     
 Kernel Type:                      tc                       
 Loaded At:                        2025-07-22T08:16:06+0000 
 Tag:                              d796b57bdaf88123         
 GPL Compatible:                   true                     
 Map IDs:                          [27]                     
 BTF ID:                           101                      
 Size Translated (bytes):          96                       
 JITted:                           true                     
 Size JITted:                      76                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       9
```

For the `--programs tc:pass` program loaded with the command above, the `<FUNC_NAME>`
would be set as shown in the following snippet, taken from the function name,
not `SEC()`:

```c
SEC("classifier/pass")
int pass(struct __sk_buff *skb) {
{
    :
}
```

#### Loading From Remote Repository

Below is an example loading an eBPF program packaged in an OCI container image
from a given registry:

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest \
     --programs xdp:pass --application XdpPassProgram
 Bpfman State                                           
 BPF Function:  pass                                    
 Program Type:  xdp                                     
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest 
 Pull Policy:   IfNotPresent                            
 Global:        None                                    
 Metadata:      bpfman_application=XdpPassProgram       
 Map Pin Path:  /run/bpfman/fs/maps/184                 
 Map Owner ID:  None                                    
 Maps Used By:  184                                     
 Links:         None                                    

 Kernel State                                               
 Program ID:                       184                      
 BPF Function:                     pass                     
 Kernel Type:                      xdp                      
 Loaded At:                        2025-07-22T08:22:05+0000 
 Tag:                              4b9d1b2c140e87ce         
 GPL Compatible:                   true                     
 Map IDs:                          [60]                     
 BTF ID:                           188                      
 Size Translated (bytes):          96                       
 JITted:                           true                     
 Size JITted:                      79                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       9 
```

#### Loading Multiple Programs

An eBPF bytecode image can contain multiple programs.
Below is an example of how to load multiple eBPF programs in one load command.
Commands that are loaded together can share global data and maps.
Optionally, include an `application` name that can be used to group the load
programs together when displaying.

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/go-app-counter:latest \
   --programs kprobe:kprobe_counter tracepoint:tracepoint_kill_recorder tc:stats \
              tcx:tcx_stats uprobe:uprobe_counter xdp:xdp_stats --application go-app
 Program ID  Application  Type        Function Name    Links 
 224         go-app       kprobe      kprobe_counter         
 225         go-app       tracepoint  tracepoint_kill        
 226         go-app       tc          stats                  
 227         go-app       tcx         tcx_stats              
 228         go-app       uprobe      uprobe_counter         
 229         go-app       xdp         xdp_stats 
```

#### Loading fentry/fexit Programs

Below is an example loading an fentry program (fexit is similar).
The fentry and fexit commands require the attach point at load time, which
is the name of the function the eBPF will be attached too.
The function name is included in the `--programs` parameter and uses the
format: `<TYPE>:<FUNC_NAME>:<ATTACH_FUNC>`
The fentry and fexit programs still require a `bpfman attach` command to be
called before they will actually be triggered.

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/fentry:latest \
     --programs fentry:test_fentry:do_unlinkat
 Bpfman State                                         
 BPF Function:  test_fentry                           
 Program Type:  fentry                                
 Image URL:     quay.io/bpfman-bytecode/fentry:latest 
 Pull Policy:   IfNotPresent                          
 Global:        None                                  
 Metadata:      None                                  
 Map Pin Path:  /run/bpfman/fs/maps/244               
 Map Owner ID:  None                                  
 Maps Used By:  244                                   
 Links:         None                                  

 Kernel State                                               
 Program ID:                       244                      
 BPF Function:                     test_fentry              
 Kernel Type:                      tracing                  
 Loaded At:                        2025-07-22T08:25:37+0000 
 Tag:                              dda189308c8908           
 GPL Compatible:                   true                     
 Map IDs:                          [93]                     
 BTF ID:                           253                      
 Size Translated (bytes):          48                       
 JITted:                           true                     
 Size JITted:                      48                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       5

$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/fentry:latest \
     --programs fexit:test_fexit:do_unlinkat
 Bpfman State                                         
 BPF Function:  test_fexit                            
 Program Type:  fexit                                 
 Image URL:     quay.io/bpfman-bytecode/fentry:latest 
 Pull Policy:   IfNotPresent                          
 Global:        None                                  
 Metadata:      None                                  
 Map Pin Path:  /run/bpfman/fs/maps/252               
 Map Owner ID:  None                                  
 Maps Used By:  252                                   
 Links:         None                                  

 Kernel State                                               
 Program ID:                       252                      
 BPF Function:                     test_fexit               
 Kernel Type:                      tracing                  
 Loaded At:                        2025-07-22T08:26:29+0000 
 Tag:                              85719cd36bd53c46         
 GPL Compatible:                   true                     
 Map IDs:                          [97]                     
 BTF ID:                           262                      
 Size Translated (bytes):          48                       
 JITted:                           true                     
 Size JITted:                      48                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       5
```

#### Loading probe Versus retprobe Programs

`kprobe` and `kretprobe` (as well as `uprobe` and `uretprobe`) are loaded and attached
with the same set of attributes.
From the kernel's perspective, probes and retprobes are both probes.
What distinguishes a probe from a retprobe is the `SEC(..)` header in the code.
For example, `kprobe` will look something like:

```C
SEC("kprobe/my_kprobe")
int my_kprobe(struct pt_regs *ctx) {
  bpf_printk(" KP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return 0;
}
```

Whereas `kretprobe` may look something like:

```c
SEC("kretprobe/my_kretprobe")
int my_kretprobe(struct pt_regs *ctx) {
  bpf_printk("KRP: GLOBAL_u8: 0x%02X, GLOBAL_u32: 0x%08X", GLOBAL_u8,
             GLOBAL_u32);
  return 0;
}
```

But loading each program type is similar:

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/kprobe:latest \
   --programs kprobe:my_kprobe
 Bpfman State                                         
 BPF Function:  my_kprobe                             
 Program Type:  kprobe                                
 Image URL:     quay.io/bpfman-bytecode/kprobe:latest 
 Pull Policy:   IfNotPresent                          
 Global:        None                                  
 Metadata:      None                                  
 Map Pin Path:  /run/bpfman/fs/maps/81                
 Map Owner ID:  None                                  
 Maps Used By:  81                                    
 Links:         None                                  

 Kernel State                                               
 Program ID:                       81                       
 BPF Function:                     my_kprobe                
 Kernel Type:                      probe                    
 Loaded At:                        2025-07-22T11:30:57+0000 
 Tag:                              9b2c38d37350bfff         
 GPL Compatible:                   true                     
 Map IDs:                          [24]                     
 BTF ID:                           82                       
 Size Translated (bytes):          96                       
 JITted:                           true                     
 Size JITted:                      76                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       9
```

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/kretprobe:latest \
   --programs kprobe:my_kretprobe
 Bpfman State                                            
 BPF Function:  my_kretprobe                             
 Program Type:  kprobe                                   
 Image URL:     quay.io/bpfman-bytecode/kretprobe:latest 
 Pull Policy:   IfNotPresent                             
 Global:        None                                     
 Metadata:      None                                     
 Map Pin Path:  /run/bpfman/fs/maps/89                   
 Map Owner ID:  None                                     
 Maps Used By:  89                                       
 Links:         None                                     

 Kernel State                                               
 Program ID:                       89                       
 BPF Function:                     my_kretprobe             
 Kernel Type:                      probe                    
 Loaded At:                        2025-07-22T11:31:45+0000 
 Tag:                              9b2c38d37350bfff         
 GPL Compatible:                   true                     
 Map IDs:                          [28]                     
 BTF ID:                           91                       
 Size Translated (bytes):          96                       
 JITted:                           true                     
 Size JITted:                      76                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       9
```

### Setting Global Variables in eBPF Programs

Global variables can be set for any eBPF program type when loading as follows:

```console
$ cd bpfman/
$ sudo bpfman load file -p tests/integration-test/bpf/.output/tc_pass.bpf/bpf_x86_bpfel.o \
     --programs tc:pass -g GLOBAL_u8=01 GLOBAL_u32=0A0B0C0D --application TcGlobal
 Bpfman State                                                                  
 BPF Function:  pass                                                           
 Program Type:  tc                                                             
 Path:          tests/integration-test/bpf/.output/tc_pass.bpf/bpf_x86_bpfel.o 
 Global:        GLOBAL_u8=01                                                   
                GLOBAL_u32=0A0B0C0D                                            
 Metadata:      bpfman_application=TcGlobal                                    
 Map Pin Path:  /run/bpfman/fs/maps/98                                         
 Map Owner ID:  None                                                           
 Maps Used By:  98                                                             
 Links:         None                                                           

 Kernel State                                               
 Program ID:                       98                       
 BPF Function:                     pass                     
 Kernel Type:                      tc                       
 Loaded At:                        2025-07-22T11:32:57+0000 
 Tag:                              d796b57bdaf88123         
 GPL Compatible:                   true                     
 Map IDs:                          [32]                     
 BTF ID:                           109                      
 Size Translated (bytes):          96                       
 JITted:                           true                     
 Size JITted:                      76                       
 Kernel Allocated Memory (bytes):  4096                     
 Verified Instruction Count:       9
```

Note that when setting global variables, the eBPF program being loaded must
have global variables named with the strings given, and the size of the value
provided must match the size of the given variable.  For example, the above
command can be used to update the following global variables in an eBPF program
(see [tc_pass.bpf.c](https://github.com/bpfman/bpfman/blob/main/tests/integration-test/bpf/tc_pass.bpf.c)).

```c
volatile const __u32 GLOBAL_u8 = 0;
volatile const __u32 GLOBAL_u32 = 0;
```

## bpfman attach

The `bpfman attach` command is used to attach an eBPF program to a hook point.
Each program type (i.e. `<COMMAND>`) has it's own set of attributes specific to the program type,
and those program specific attributes MUST come after the `Program ID` (from the load command) and
the program type are entered.

```console
$ sudo bpfman attach --help
Attach an eBPF program to a hook point using the Program Id

Usage: bpfman attach <PROGRAM_ID> <COMMAND>

Commands:
  xdp         Install an eBPF program on the XDP hook point for a given interface
  tc          Install an eBPF program on the TC hook point for a given interface
  tcx         Install an eBPF program on the TCX hook point for a given interface and direction
  tracepoint  Install an eBPF program on a Tracepoint
  kprobe      Install a kprobe or kretprobe eBPF probe
  uprobe      Install a uprobe or uretprobe eBPF probe
  fentry      Install a fentry eBPF probe
  fexit       Install a fexit eBPF probe
  help        Print this message or the help of the given subcommand(s)

Arguments:
  <PROGRAM_ID>  Required: Program Id to be attached

Options:
  -h, --help  Print help
```

Each `<COMMAND>` has its own custom parameters:

```console
$ sudo bpfman attach xdp --help
Install an eBPF program on the XDP hook point for a given interface

Usage: bpfman attach <PROGRAM_ID> xdp [OPTIONS] --iface <IFACE> --priority <PRIORITY>

Options:
  -i, --iface <IFACE>
          Required: Interface to load program on

  -p, --priority <PRIORITY>
          Required: Priority to run program in chain. Lower value runs first.
          [possible values: 1-1000]

      --proceed-on <PROCEED_ON>...
          Optional: Proceed to call other programs in chain on this exit code.
          Multiple values supported by repeating the parameter.
          Example: --proceed-on pass --proceed-on drop

          [possible values: aborted, drop, pass, tx, redirect, dispatcher_return]

          [default: pass, dispatcher_return]

  -n, --netns <NETNS>
          Optional: The file path of the target network namespace.
          Example: -n /var/run/netns/bpfman-test

  -m, --metadata <METADATA>
          Optional: Specify Key/Value metadata to be attached to a link when it
          is loaded by bpfman.
          Format: <KEY>=<VALUE>

          This can later be used to list a certain subset of links which contain
          the specified metadata.
          Example: --metadata owner=acme

  -h, --help
          Print help (see a summary with '-h')```

Example attaching an XDP Program:

```console
$ bpfman attach 184 xdp --iface enp1s0 --priority 100
 Bpfman State                                          
 BPF Function:       pass                              
 Program Type:       xdp                               
 Program ID:         184                               
 Link ID:            713799068                         
 Interface:          enp1s0                            
 Priority:           100                               
 Position:           0                                 
 Proceed On:         pass, dispatcher_return           
 Network Namespace:  None                              
 Metadata:           bpfman_application=XdpPassProgram
```

The `tc` command is similar to `xdp`, but it also requires the `direction` option
and the `proceed-on` values are different.

```console
$ sudo bpfman attach tc --help
Install an eBPF program on the TC hook point for a given interface

Usage: bpfman attach <PROGRAM_ID> tc [OPTIONS] --direction <DIRECTION> --iface <IFACE> --priority <PRIORITY>

Options:
  -d, --direction <DIRECTION>
          Required: Direction to apply program.

          [possible values: ingress, egress]

  -i, --iface <IFACE>
          Required: Interface to load program on

  -p, --priority <PRIORITY>
          Required: Priority to run program in chain. Lower value runs first.
          [possible values: 1-1000]

      --proceed-on <PROCEED_ON>...
          Optional: Proceed to call other programs in chain on this exit code.
          Multiple values supported by repeating the parameter.
          Example: --proceed-on ok --proceed-on pipe

          [possible values: unspec, ok, reclassify, shot, pipe, stolen, queued,
                            repeat, redirect, trap, dispatcher_return]

          [default: ok, pipe, dispatcher_return]

  -n, --netns <NETNS>
          Optional: The file path of the target network namespace.
          Example: -n /var/run/netns/bpfman-test

  -m, --metadata <METADATA>
          Optional: Specify Key/Value metadata to be attached to a link when it
          is loaded by bpfman.
          Format: <KEY>=<VALUE>

          This can later be used to list a certain subset of links which contain
          the specified metadata.
          Example: --metadata owner=acme

  -h, --help
          Print help (see a summary with '-h')
```

The following is an example of attaching the `tc` command using short option names:

```console
$ sudo bpfman attach 98 tc -d ingress -i enp1s0 -p 40
 Bpfman State                                    
 BPF Function:       pass                        
 Program Type:       tc                          
 Program ID:         98                          
 Link ID:            4210414695                  
 Interface:          enp1s0                      
 Direction:          ingress                     
 Priority:           40                          
 Position:           0                           
 Proceed On:         pipe, dispatcher_return     
 Network Namespace:  None                        
 Metadata:           bpfman_application=TcGlobal
```

### Additional Attach Examples

Below are some additional examples of `bpfman attach` commands:

#### Fentry

```console
sudo bpfman attach 63682 fentry
```

#### Fexit

```console
sudo bpfman attach 63744 fexit
```

#### Kprobe

```console
sudo bpfman attach 63690 kprobe -f try_to_wake_up
```

#### Kretprobe

```console
sudo bpfman attach 63698 kprobe -f try_to_wake_up
```

#### TC

```console
sudo bpfman attach 63706 tc --direction ingress --iface eno3 --priority 110
```

#### TCX

```console
sudo bpfman attach 63672 tcx --direction ingress --iface eno3 --priority 22
```

#### Tracepoint

```console
sudo bpfman attach 63670 tracepoint --tracepoint syscalls/sys_enter_openat
```

#### Uprobe

```console
sudo bpfman attach 63673 uprobe -t "libc" -f "malloc"
```

#### Uretprobe

```console
sudo bpfman attach 63809 uprobe -t "libc" -f "malloc"
```

#### XDP

```console
sudo bpfman attach 63674 xdp --iface eno3 --priority 35
```

### Attach to Multiple Hook Points

Most programs can attach to multiple hook points.
To attach a program to multiple hook points, simply call `bpfman attach`
multiple times with the same `Program ID`:

```console
$ sudo bpfman attach 63661 xdp --iface eno3 --priority 35
$ sudo bpfman attach 63661 xdp --iface eno4 --priority 35

$ sudo bpfman list programs --application XdpPassProgram
 Program ID  Application     Type  Function Name  Links
 63661       XdpPassProgram  xdp   pass           (2) 1301256968, 18827142
```

### Modifying the Proceed-On Behavior

The `proceed-on` setting applies to `xdp` and `tc` programs. For both of these
program types, an ordered list of eBPF programs is maintained per attach point.
The `proceed-on` setting determines whether processing will "proceed" to the
next eBPF program in the list, or terminate processing and return, based on the
program's return value. For example, the default `proceed-on` configuration for
an `xdp` program can be modified as follows:

```console
sudo bpfman attach 63661 xdp -i eno3 -p 30 --proceed-on drop pass dispatcher_return
```

## bpfman detach

The `bpfman detach` command is used to detach an eBPF program from a hook point.
When detached, the eBPF program is still loaded in kernel memory, but it is not
attached to the hook point, so the eBPF program will not be triggered.
The `bpfman detach` takes the `Link ID`, which can be obtained from the
`bpfman list programs|links` or `bpfman get program|link` commands.

```console
$ sudo bpfman detach --help
Detach an eBPF program from a hook point using the Link Id

Usage: bpfman detach <LINK_ID>

Arguments:
  <LINK_ID>  Required: Link Id to be detached

Options:
  -h, --help  Print help
```

For example:

```console
$ sudo bpfman list programs
 Program ID  Application     Type        Function Name    Links
 63652       TcPassProgram   tc          pass
 63661       XdpPassProgram  xdp         pass             (3) 1301256968, 18827142, 3974774760
 63669       go-app          kprobe      kprobe_counter
 63670       go-app          tracepoint  tracepoint_kill  (1) 1462192047
 63671       go-app          tc          stats            (1) 3041462868
 63672       go-app          tcx         tcx_stats        (1) 3926782293
 63673       go-app          uprobe      uprobe_counter
 63674       go-app          xdp         xdp_stats        (2) 241636937, 4229414503
 63682                       fentry      test_fentry      (1) 294437142
 63690                       kprobe      my_kprobe        (1) 2131925936
 63698                       kprobe      my_kretprobe     (1) 1834679786
 63706       TcGlobal        tc          pass             (1) 2333059649
 63744                       fexit       test_fexit       (1) 2055942218
 63809                       uprobe      uretprobe_count  (1) 800266964
```

```console
sudo bpfman detach 3974774760
```

## bpfman list

The `bpfman list programs` command lists all the bpfman loaded eBPF programs and
the `bpfman list links` command lists all the bpfman attached eBPF programs.

### bpfman list programs

Use the `bpfman list programs` command lists all the bpfman loaded eBPF programs.
From the output of the command, if there is a value for the `Links` parameter,
then the program has been loaded and attached.
If no value exists, the program has only been loaded (or is not managed by bpfman).

```console
$ sudo bpfman list programs
 Program ID  Application     Type        Function Name    Links
 63652       TcPassProgram   tc          pass
 63661       XdpPassProgram  xdp         pass             (2) 1301256968, 18827142
 63669       go-app          kprobe      kprobe_counter
 63670       go-app          tracepoint  tracepoint_kill  (1) 1462192047
 63671       go-app          tc          stats            (1) 3041462868
 63672       go-app          tcx         tcx_stats        (1) 3926782293
 63673       go-app          uprobe      uprobe_counter
 63674       go-app          xdp         xdp_stats        (2) 241636937, 4229414503
 63682                       fentry      test_fentry      (1) 294437142
 63690                       kprobe      my_kprobe        (1) 2131925936
 63698                       kprobe      my_kretprobe     (1) 1834679786
 63706       TcGlobal        tc          pass             (1) 2333059649
 63744                       fexit       test_fexit       (1) 2055942218
 63809                       uprobe      uretprobe_count  (1) 800266964
```

If the `--application` parameter was used during the `bpfman load` command, that
can be used to filter the programs displayed in the command.

```console
$ sudo bpfman list programs --application go-app
 Program ID  Application  Type        Function Name    Links
 63669       go-app       kprobe      kprobe_counter
 63670       go-app       tracepoint  tracepoint_kill  (1) 1462192047
 63671       go-app       tc          stats            (1) 3041462868
 63672       go-app       tcx         tcx_stats        (1) 3926782293
 63673       go-app       uprobe      uprobe_counter
 63674       go-app       xdp         xdp_stats        (2) 241636937, 422941450
 ```

To see all eBPF programs loaded on the system, not just bpfman loaded programs,
include the `--all` option.

```console
$ sudo bpfman list programs --all
 :
 63638                       cgroup_device  sd_devices
 63639                       cgroup_skb     sd_fw_egress
 63640                       cgroup_skb     sd_fw_ingress
 63641                       cgroup_device  sd_devices
 63642                       cgroup_skb     sd_fw_egress
 63643                       cgroup_skb     sd_fw_ingress
 63651                       tc             tc_dispatcher
 63652       TcPassProgram   tc             pass
 63660                       xdp            xdp_dispatcher
 63661       XdpPassProgram  xdp            pass             (2) 1301256968, 18827142
 63669       go-app          kprobe         kprobe_counter
 63670       go-app          tracepoint     tracepoint_kill  (1) 1462192047
 63671       go-app          tc             stats            (1) 3041462868
 63672       go-app          tcx            tcx_stats        (1) 3926782293
 63673       go-app          uprobe         uprobe_counter
 63674       go-app          xdp            xdp_stats        (2) 241636937, 4229414503
 63682                       fentry         test_fentry      (1) 294437142
 63690                       kprobe         my_kprobe        (1) 2131925936
 63698                       kprobe         my_kretprobe     (1) 1834679786
 63706       TcGlobal        tc             pass             (1) 2333059649
 63744                       fexit          test_fexit       (1) 2055942218
 63787                       tc             tc_dispatcher
 63809                       uprobe         uretprobe_count  (1) 800266964
 63847                       xdp            xdp_dispatcher
 63884                       xdp            xdp_dispatcher
 ```

To filter on a given program type, include the `--program-type` parameter:

```console
$ sudo bpfman list programs --all --program-type tc
 Program ID  Application    Type  Function Name  Links
 63651                      tc    tc_dispatcher
 63652       TcPassProgram  tc    pass
 63671       go-app         tc    stats          (1) 3041462868
 63672       go-app         tcx   tcx_stats      (1) 3926782293
 63706       TcGlobal       tc    pass           (1) 2333059649
 63787                      tc    tc_dispatcher
```

**Note:** The list filters by the Kernel Program Type.

* **probe**: `kprobe`, `kretprobe`, `uprobe` and `uretprobe` all map to the `probe` Kernel Program Type.
* **tracing**: `fentry` and `fexit` both map to the `tracing` Kernel Program Type.
* **tc**: `tc` and `tcx` both map to the `tc` Kernel Program Type.
* For all possible program type values, see `bpfman list programs --help`.

### bpfman list links

Use the `bpfman list links` command lists all the bpfman attached eBPF programs.

```console
$ sudo bpfman list links
 Program ID  Link ID     Application     Type        Function Name    Attachment
 63661       1301256968  XdpPassProgram  xdp         pass             eno4 pos-0
 63661       18827142    XdpPassProgram  xdp         pass             eno3 pos-0
 63670       1462192047  go-app          tracepoint  tracepoint_kill  syscalls/sys_enter_openat
 63671       3041462868  go-app          tc          stats            eno3 ingress pos-0
 63672       3926782293  go-app          tcx         tcx_stats        eno3 ingress pos-0
 63674       241636937   go-app          xdp         xdp_stats        eno3 pos-2
 63674       4229414503  go-app          xdp         xdp_stats        eno3 pos-1
 63682       294437142                   fentry      test_fentry      do_unlinkat
 63690       2131925936                  kprobe      my_kprobe        try_to_wake_up
 63698       1834679786                  kprobe      my_kretprobe     try_to_wake_up
 63706       2333059649  TcGlobal        tc          pass             eno3 ingress pos-1
 63744       2055942218                  fexit       test_fexit       do_unlinkat
 63809       800266964                   uprobe      uretprobe_count  libc malloc
```

If the `--application` parameter was used during the `bpfman load` command, that
can be used to filter the programs displayed in the command.

```console
$ sudo bpfman list links --application go-app
 Program ID  Link ID     Application  Type        Function Name    Attachment
 63670       1462192047  go-app       tracepoint  tracepoint_kill  syscalls/sys_enter_openat
 63671       3041462868  go-app       tc          stats            eno3 ingress pos-0
 63672       3926782293  go-app       tcx         tcx_stats        eno3 ingress pos-0
 63674       241636937   go-app       xdp         xdp_stats        eno3 pos-2
 63674       4229414503  go-app       xdp         xdp_stats        eno3 pos-1
 ```

To filter on a given program type, include the `--program-type` parameter:

```console
$ sudo bpfman list links -p tc
 Program ID  Link ID     Application  Type  Function Name  Attachment
 63671       3041462868  go-app       tc    stats          eno3 ingress pos-0
 63672       3926782293  go-app       tcx   tcx_stats      eno3 ingress pos-0
 63706       2333059649  TcGlobal     tc    pass           eno3 ingress pos-1
```

**Note:** The list filters by the Kernel Program Type.

* **probe**: `kprobe`, `kretprobe`, `uprobe` and `uretprobe` all map to the `probe` Kernel Program Type.
* **tracing**: `fentry` and `fexit` both map to the `tracing` Kernel Program Type.
* **tc**: `tc` and `tcx` both map to the `tc` Kernel Program Type.

## bpfman get

To retrieve detailed information for a loaded eBPF program, use the
`bpfman get program <PROGRAM_ID>` command.
To retrieve detailed information for an attached eBPF program, use the
`bpfman get link <LINK_ID>` command.

### bpfman get program

To retrieve detailed information for a loaded eBPF program, use the
`bpfman get program <PROGRAM_ID>` command.
If the eBPF program was loaded via bpfman, then there will be a `Bpfman State`
section with bpfman related attributes and a `Kernel State` section with
kernel information.
If the eBPF program was loaded outside of bpfman, then the `Bpfman State`
section will be empty and `Kernel State` section will be populated.

bpfman managed eBPF Program:

```console
$ sudo bpfman get program 63661
 Bpfman State
---------------
 BPF Function:  pass
 Program Type:  xdp
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      bpfman_application=XdpPassProgram
 Map Pin Path:  /run/bpfman/fs/maps/63661
 Map Owner ID:  None
 Maps Used By:  63661
 Links:         1301256968 (eno4 pos-0)
                18827142 (eno3 pos-0)
 Kernel State
----------------------------------
 Program ID:                       63661
 BPF Function:                     pass
 Kernel Type:                      xdp
 Loaded At:                        2025-04-01T10:26:22-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [21083]
 BTF ID:                           31353
 Size Translated (bytes):          96
 JITted:                           true
 Size JITted:                      75
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

Non-bpfman managed eBPF Program:

```console
$ sudo bpfman get program 63643
 Kernel State
----------------------------------
 Program ID:                       63643
 BPF Function:                     sd_fw_ingress
 Kernel Type:                      cgroup_skb
 Loaded At:                        2025-04-01T10:25:02-0400
 Tag:                              6deef7357e7b4530
 GPL Compatible:                   true
 Map IDs:                          []
 BTF ID:                           0
 Size Translated (bytes):          64
 JITted:                           true
 Size JITted:                      63
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       8
```

### bpfman get link

To retrieve detailed information for an attached eBPF program, use the
`bpfman get link <LINK_ID>` command.
Only bpfman loaded and attached eBPF programs contain link data.

```console
$ sudo bpfman get link 18827142
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63661
 Link ID:            18827142
 Interface:          eno3
 Priority:           35
 Position:           0
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

## bpfman unload

The `bpfman unload` command takes the `Program ID` from the load or list command as a parameter,
and unloads the requested eBPF program.
The eBPF programs do not need to be detached before unloading.

```console
sudo bpfman unload 63661
```

```console
$ sudo bpfman list programs
 Program ID  Application    Type        Function Name    Links
 63652       TcPassProgram  tc          pass
 63669       go-app         kprobe      kprobe_counter
 63670       go-app         tracepoint  tracepoint_kill  (1) 1462192047
 63671       go-app         tc          stats            (1) 3041462868
 63672       go-app         tcx         tcx_stats        (1) 3926782293
 63673       go-app         uprobe      uprobe_counter
 63674       go-app         xdp         xdp_stats        (2) 241636937, 4229414503
 63682                      fentry      test_fentry      (1) 294437142
 63690                      kprobe      my_kprobe        (1) 2131925936
 63698                      kprobe      my_kretprobe     (1) 1834679786
 63706       TcGlobal       tc          pass             (1) 2333059649
 63744                      fexit       test_fexit       (1) 2055942218
 63809                      uprobe      uretprobe_count  (1) 800266964
```

## bpfman image

The `bpfman image` commands contain a set of container image related commands.

### bpfman image pull

The `bpfman image pull` command pulls a given bytecode image for future use
by a load command.

```console
$ sudo bpfman image pull --help
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
$ sudo bpfman image pull --image-url quay.io/bpfman-bytecode/xdp_pass:latest
Successfully downloaded bytecode
```

Then when loaded, the local image will be used:

```console
$ sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest \
     --programs xdp:pass
 Bpfman State
 ---------------
 BPF Function:  pass
 Program Type:  xdp
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      None
 Map Pin Path:  /run/bpfman/fs/maps/64047
 Map Owner ID:  None
 Maps Used By:  64047
 Links:         None

 Kernel State
 ----------------------------------
 Program ID:                       64047
 BPF Function:                     pass
 Kernel Type:                      xdp
 Loaded At:                        2025-04-01T12:32:51-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [21259]
 BTF ID:                           31787
 Size Translated (bytes):          96
 JITted:                           true
 Size JITted:                      75
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
