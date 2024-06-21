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

bpfman is leveraging the libxdp protocol to allow it's users to load up to 10
XDP programs on a given interface.
This tutorial will show you how to use `bpfman` to load multiple XDP programs
on an interface.

!!! Note:
    The TC hook point is also associated with an interface.
    Within bpfman, TC is implemented in a similar fashion to XDP in that it uses a dispatcher with
    stub functions.
    TCX is a fairly new kernel feature that improves how the kernel handles multiple TC programs
    on a given interface.
    bpfman is on the process of integrating TCX support, which will replace the dispatcher logic
    for TC.
    Until then, assume TC behaves in a similar fashion to XDP.

See [Launching bpfman](../getting-started/launching-bpfman.md)
for more detailed instructions on building and loading bpfman.
This tutorial assumes bpfman has been built and the `bpfman` CLI is in $PATH.

## Load XDP program

We will load the simple `xdp-pass` program, which permits all traffic to the attached interface,
`eno3` in this example.
We will use the priority of 100.
Find a deeper dive into CLI syntax in [CLI Guide](../getting-started/cli-guide.md).

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --name pass \
  xdp --iface eno3 --priority 100
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
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6213
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
From the output, the Program Id of `6213` can be found in the `Kernel State` section.
This id can be used to perform a `bpfman get` to retrieve all relevant program
data and a `bpfman unload` when the program needs to be unloaded.

```console
sudo bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400
```

We can recheck the details about the loaded program with the `bpfman get` command:

```console
sudo bpfman get 6213
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
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6213
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

## Loading Additional XDP Programs

We will now load 2 more programs with different priorities to demonstrate how bpfman
will ensure they are ordered correctly:

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --name pass \
  xdp --iface eno3 --priority 50
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
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6215
 Name:                             pass
 Type:                             xdp
:
```

```console
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --name pass \
  xdp --iface eno3 --priority 200
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
 Iface:         eno3
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6217
 Name:                             pass
 Type:                             xdp
:
```

Using `bpfman list` we can see all the programs that were loaded.

```console
sudo bpfman list
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
sudo bpfman get 6213
 Bpfman State
---------------
 Name:          pass
:
 Priority:      100
 Iface:         eno3
 Position:      1
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6213
 Name:                             pass
 Type:                             xdp
:
```

```console
sudo bpfman get 6215
 Bpfman State
---------------
 Name:          pass
:
 Priority:      50
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6215
 Name:                             pass
 Type:                             xdp
:
```

```console
sudo bpfman get 6217
 Bpfman State
---------------
 Name:          pass
:
 Priority:      200
 Iface:         eno3
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6217
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
sudo bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --name pass \
  xdp --iface eno3 --priority 150 --proceed-on "pass" --proceed-on "dispatcher_return"
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
 Iface:         eno3
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6219
 Name:                             pass
 Type:                             xdp
:
```

Which results in being loaded in position `2` because it was loaded at priority `150`,
which is lower than the previous program at that position with a priority of `200`.

## Delete XDP Program

Let's remove the program at position 1.

```console
sudo bpfman list
 Program ID  Name  Type  Load Time
 6213        pass  xdp   2023-07-17T17:48:10-0400
 6215        pass  xdp   2023-07-17T17:52:46-0400
 6217        pass  xdp   2023-07-17T17:53:57-0400
 6219        pass  xdp   2023-07-17T17:59:41-0400
```

```console
sudo bpfman unload 6213
```

And we can verify that it has been removed and the other programs re-ordered:

```console
sudo bpfman list
 Program ID  Name  Type  Load Time
 6215        pass  xdp   2023-07-17T17:52:46-0400
 6217        pass  xdp   2023-07-17T17:53:57-0400
 6219        pass  xdp   2023-07-17T17:59:41-0400
```

```console
bpfman get 6215
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
 Iface:         eno3
 Position:      0
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6215
 Name:                             pass
 Type:                             xdp
:
```

```
bpfman get 6217
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
 Iface:         eno3
 Position:      2
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6217
 Name:                             pass
 Type:                             xdp
:
```

```
bpfman get 6219
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
 Iface:         eno3
 Position:      1
 Proceed On:    pass, dispatcher_return

 Kernel State
----------------------------------
 Program ID:                       6219
 Name:                             pass
 Type:                             xdp
:
```
