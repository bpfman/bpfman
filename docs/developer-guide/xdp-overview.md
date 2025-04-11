#  XDP Tutorial

The XDP hook point is unique in that the associated eBPF program attaches to
an interface and only one eBPF program is allowed to attach to the XDP hook
point for a given interface.
Due to this limitation, the
[libxdp protocol](https://github.com/xdp-project/xdp-tools/blob/master/lib/libxdp/protocol.org)
was written.
The one program that is attached to the XDP hook point is an eBPF dispatcher
program.
The dispatcher program contains a list of 10 stub functions.
When XDP programs wish to be loaded, they are loaded as extension programs
which are then called in place of one of the stub functions.

bpfman is leveraging the libxdp protocol and dispatcher program to allow it's
users to load up to 10 XDP programs on a given interface.
This tutorial will show you how to use `bpfman` to load multiple XDP programs
on an interface.

!!! Note
    The TC hook point is also associated with an interface.
    Within bpfman, TC is implemented in a similar fashion to XDP in that it uses a dispatcher with
    stub functions.
    TCX is a fairly new kernel feature that improves how the kernel handles multiple TC programs
    on a given interface and does not use the dispatcher program.

See [Launching bpfman](../getting-started/launching-bpfman.md)
for more detailed instructions on building and loading bpfman.
This tutorial assumes bpfman has been built and the `bpfman` CLI is in $PATH.

## Load XDP program

We will load and attach the simple `xdp-pass` program, which permits all traffic to
the attached interface, `eno3` in this example.
We will use the priority of 100.
Find a deeper dive into CLI syntax in [CLI Guide](../getting-started/cli-guide.md).

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --application XdpPassProgram \
     --programs xdp:pass
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      bpfman_application=XdpPassProgram
 Map Pin Path:  /run/bpfman/fs/maps/63336
 Map Owner ID:  None
 Maps Used By:  63336
 Links:         None

 Kernel State
----------------------------------
 Program ID:                       63336
 Name:                             pass
 Type:                             xdp
 Loaded At:                        2025-03-31T17:49:22-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [21009]
 BTF ID:                           31179
 Size Translated (bytes):          96
 JITted:                           true
 Size JITted:                      75
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

Using the `Program ID` from the output from the `bpfman load` command,
the eBPF Program can then be attached to the interface.

```console
sudo bpfman attach 63336 xdp --iface eno3 --priority 100
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            3736854134
 Interface:          eno3
 Priority:           100
 Position:           0
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

`bpfman load image` returns the same data as a `bpfman get program` command and the
`bpfman attach` returns the same data as a `bpfman get link` command.
From the output, the `Program ID` of `63336` can be found in the `Kernel State` section.
This id can be used to perform a `bpfman get program` to retrieve all relevant program
data and a `bpfman unload` when the program needs to be unloaded.

```console
sudo bpfman list programs
 Program ID  Application     Type  Function Name  Links
 63336       XdpPassProgram  xdp   pass           (1) 3736854134
```

We can recheck the details about the loaded program with the `bpfman get program` command:

```console
sudo bpfman get program 63336
 Bpfman State
---------------
 Name:          pass
 Image URL:     quay.io/bpfman-bytecode/xdp_pass:latest
 Pull Policy:   IfNotPresent
 Global:        None
 Metadata:      bpfman_application=XdpPassProgram
 Map Pin Path:  /run/bpfman/fs/maps/63336
 Map Owner ID:  None
 Maps Used By:  63336
 Links:         3736854134 (eno3 pos-0)

 Kernel State
----------------------------------
 Program ID:                       63336
 Name:                             pass
 Type:                             xdp
 Loaded At:                        2025-03-31T17:49:22-0400
 Tag:                              4b9d1b2c140e87ce
 GPL Compatible:                   true
 Map IDs:                          [21009]
 BTF ID:                           31179
 Size Translated (bytes):          96
 JITted:                           true
 Size JITted:                      75
 Kernel Allocated Memory (bytes):  4096
 Verified Instruction Count:       9
```

We can also recheck the details about the attached program with the `bpfman get link` command:

```console
sudo bpfman get link 3736854134
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            3736854134
 Interface:          eno3
 Priority:           100
 Position:           0
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

From the link output above you can see the program was loaded to position 0 on our
interface and thus will be executed first.

## Loading Additional XDP Programs

We will now attach 2 more programs with different priorities to demonstrate how bpfman
will ensure they are ordered correctly:

```console
sudo bpfman attach 63336 xdp --iface eno3 --priority 50
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            155072461
 Interface:          eno3
 Priority:           50
 Position:           0
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

```console
sudo bpfman attach 63336 xdp --iface eno3 --priority 200
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            454777406
 Interface:          eno3
 Priority:           200
 Position:           2
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

Using `bpfman list links` we can see all the programs that were attached.

```console
sudo bpfman list links --application XdpPassProgram
 Program ID  Link ID     Application     Type  Function Name  Attachment
 63336       155072461   XdpPassProgram  xdp   pass           eno3 pos-0
 63336       3736854134  XdpPassProgram  xdp   pass           eno3 pos-1
 63336       454777406   XdpPassProgram  xdp   pass           eno3 pos-2
```

The lowest priority program is executed first, while the highest is executed last.
As can be seen from the detailed output for each command below:

* Link `155072461` is at position `0` with a priority of `50`
* Link `3736854134` is at position `1` with a priority of `100`
* Link `454777406` is at position `2` with a priority of `200`

```console
sudo bpfman get link 3736854134
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            3736854134
 Interface:          eno3
 Priority:           100
 Position:           1
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

```console
sudo bpfman get link 155072461
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            155072461
 Interface:          eno3
 Priority:           50
 Position:           0
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram

```

```console
sudo bpfman get link 454777406
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            454777406
 Interface:          eno3
 Priority:           200
 Position:           2
 Proceed On:         pass, dispatcher_return
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

By default, the next program in the chain will only be executed if a given program returns
`pass` (see `proceed-on` field in the `bpfman get link` output above).
If the next program in the chain should be called even if a different value is returned,
then the program can be loaded with those additional return values using the `proceed-on`
parameter (see `bpfman load attach xdp --help` for list of valid values):

```console
sudo bpfman attach 63336 xdp --iface eno3 --priority 150 \
   --proceed-on pass --proceed-on dispatcher_return --proceed-on drop
 Bpfman State
---------------
 BPF Function:       pass
 Program Type:       xdp
 Program ID:         63336
 Link ID:            702908334
 Interface:          eno3
 Priority:           150
 Position:           2
 Proceed On:         pass, dispatcher_return, drop
 Network Namespace:  None
 Metadata:           bpfman_application=XdpPassProgram
```

Which results in being loaded in position `2` because it was loaded at priority `150`,
which is lower than the previous program at that position with a priority of `200`.

## Delete XDP Program

Let's detach the program at position 1.

```console
sudo bpfman list links --application XdpPassProgram
 Program ID  Link ID     Application     Type  Function Name  Attachment 
 63336       155072461   XdpPassProgram  xdp   pass           eno3 pos-0
 63336       3736854134  XdpPassProgram  xdp   pass           eno3 pos-1
 63336       454777406   XdpPassProgram  xdp   pass           eno3 pos-3
 63336       702908334   XdpPassProgram  xdp   pass           eno3 pos-2
```

```console
sudo bpfman detach 3736854134
```

And we can verify that it has been removed and the other programs re-ordered:

```console
sudo bpfman list links --application XdpPassProgram
 Program ID  Link ID    Application     Type  Function Name  Attachment
 63336       155072461  XdpPassProgram  xdp   pass           eno3 pos-0
 63336       454777406  XdpPassProgram  xdp   pass           eno3 pos-2
 63336       702908334  XdpPassProgram  xdp   pass           eno3 pos-1
```
