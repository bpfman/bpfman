# Tutorial

This tutorial will show you how to use bpfd.

## Prerequisites

This tutorial uses examples from the [xdp-tutorial](https://github.com/xdp-project/xdp-tutorial).
You will need to check out the git repository and compile the examples.

## Step 1: Build bpfd

``` console
cd $HOME/src/bpfd/
cargo xtask build-ebpf --libbpf-dir $HOME/src/libbpf
cargo build
```

## Step 2: Start bpfd

While learning and experimenting with bpfd, it may be useful to run bpfd in the foreground
(which requires a second terminal to run the bpfctl commands below). For more details on 
how logging is handled in bpfd, see [Logging](#logging) below.

```console
sudo RUST_LOG=info ./target/debug/bpfd
```


Later, once familiar with bpfd, run in the background:
```console
sudo bpfd&
```

## Step 2: Load your first program

We will load the simple xdp-pass program, which permits all traffic, to the interface eth0.
The section in the object file that contains the program is "xdp".
Finally, we will use the priority of 100 - valid values are from 0 to 255.

```console
bpfctl load -p xdp -i eth0 -s "xdp" --priority 100 /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
92e3e14c-0400-4a20-be2d-f701af21873c
```

bpfctl returns a unique identifier to the program that was loaded. This may be used to detach the program later.

We can check the program was loaded using the following command:

```console
bpfctl list -i eth0
wlp2s0
xdp_mode: skb

0: 92e3e14c-0400-4a20-be2d-f701af21873c
        name: "xdp"
        priority: 100
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
```

From the output above you can see the program was loaded to slot 0 on our interface and will be executed first.


## Step 3: Loading more programs

We will now load 2 more programs with different priorities to demonstrate how bpfd will ensure they are ordered correctly:

```console
bpfctl load -p xdp -i eth0 -s "xdp" --priority 50 /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
1ccc1376-60e8-4dc5-9079-6c32748fa1c4
```

```console
bpfctl load -p xdp -i eth0 -s "xdp" --priority 200 /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
6af7c28f-6a7f-46ee-bc98-2d92ed261369
```

Using `bpfctl list` we can see that the programs are correctly ordered.
The lowest priority program is executed first, while the highest is executed last

```console
bpfctl list -i eth0
eth0
xdp_mode: skb

0: 1ccc1376-60e8-4dc5-9079-6c32748fa1c4
        name: "xdp"
        priority: 50
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
1: 92e3e14c-0400-4a20-be2d-f701af21873c
        name: "xdp"
        priority: 100
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
2: 6af7c28f-6a7f-46ee-bc98-2d92ed261369
        name: "xdp"
        priority: 200
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
```

By default, the next program in the chain will only be executed if a given program returns
`pass` (see `proceed-on` field in the `bpfctl list` output above).
If the next program in the chain should be called even if a different value is returned,
then the program can be loaded with those additional return values using the `proceed-on`
parameter:

```console
bpfctl load -p xdp -i eth0 -s "xdp" --proceed-on "drop" --proceed-on "pass" --proceed-on "dispatcher_return" --priority 150 /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
b2f19b7b-4c71-4338-873e-914bd8fa44ba
```

Which results in:

```console
bpfctl list -i eth0
eth0
xdp_mode: skb

0: 1ccc1376-60e8-4dc5-9079-6c32748fa1c4
        name: "xdp"
        priority: 50
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
1: 92e3e14c-0400-4a20-be2d-f701af21873c
        name: "xdp"
        priority: 100
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
2: b2f19b7b-4c71-4338-873e-914bd8fa44ba
        name: "xdp"
        priority: 150
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: drop, pass, dispatcher_return
3: 6af7c28f-6a7f-46ee-bc98-2d92ed261369
        name: "xdp"
        priority: 200
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass", dispatcher_return
```

## Step 4: Delete a program

Let's remove the program at slot 1.

```console
bpfctl unload -i eth0 92e3e14c-0400-4a20-be2d-f701af21873c
```

And we can verify that it has been removed and the other programs re-ordered:

```console
eth0
xdp_mode: skb

0: 1ccc1376-60e8-4dc5-9079-6c32748fa1c4
        name: "xdp"
        priority: 50
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
1: b2f19b7b-4c71-4338-873e-914bd8fa44ba
        name: "xdp"
        priority: 150
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: drop, pass, dispatcher_return
2: 6af7c28f-6a7f-46ee-bc98-2d92ed261369
        name: "xdp"
        priority: 200
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
        proceed-on: pass, dispatcher_return
```

When bpfd is stopped, all remaining programs will be unloaded automatically.

# Logging

## env_logger

bpfd and bpfctl use the [env_logger](https://docs.rs/env_logger) crate to log messages to the terminal.
By default, only `error` messages are logged, but that can be overwritten by setting
the `RUST_LOG` environment variable.
Valid values:
* `error`
* `warn`
* `info`
* `debug`
* `trace`

Example:

```console
$ sudo RUST_LOG=info /usr/local/bin/bpfd
[2022-08-08T20:29:31Z INFO  bpfd] Log using env_logger
[2022-08-08T20:29:31Z INFO  bpfd::server] Loading static programs from /etc/bpfd/programs.d
[2022-08-08T20:29:31Z INFO  bpfd::server::bpf] Map veth12fa8e3 to 13
[2022-08-08T20:29:31Z INFO  bpfd::server] Listening on [::1]:50051
[2022-08-08T20:29:31Z INFO  bpfd::server::bpf] Program added: 1 programs attached to veth12fa8e3
[2022-08-08T20:29:31Z INFO  bpfd::server] Loaded static program pass with UUID d9fd88df-d039-4e64-9f63-19f3e08915ce
```

bpfctl doesn't currently have any logs, but the infrastructure is in place if needed for future debugging.

## Systemd Service

If bpfd is running as a systemd service, then bpfd will log to journald.
As with env_logger, by default, only `error` messages are logged, but that can be
overwritten by setting the `RUST_LOG` environment variable.
bpfctl won't be run as a service, so it always uses env_logger.

Example:

```console
sudo vi /usr/lib/systemd/system/bpfd.service
[Unit]
Description=Run bpfd as a service
DefaultDependencies=no
After=network.target

[Service]
Environment="RUST_LOG=Info"
ExecStart=/home/bmcfall/src/bpfd/target/debug/bpfd

[Install]
WantedBy=sysinit.target
```

Start the service:

```console
sudo systemctl start bpfd.service
```

Check the logs:

```console
$ sudo journalctl -f -u bpfd
Aug 08 16:25:04 ebpf03 systemd[1]: Started bpfd.service - Run bpfd as a service.
Aug 08 16:25:04 ebpf03 bpfd[180118]: Log using journald
Aug 08 16:25:04 ebpf03 bpfd[180118]: Loading static programs from /etc/bpfd/programs.d
Aug 08 16:25:04 ebpf03 bpfd[180118]: Map veth12fa8e3 to 13
Aug 08 16:25:04 ebpf03 bpfd[180118]: Listening on [::1]:50051
Aug 08 16:25:04 ebpf03 bpfd[180118]: Program added: 1 programs attached to veth12fa8e3
Aug 08 16:25:04 ebpf03 bpfd[180118]: Loaded static program pass with UUID a3ffa14a-786d-48ad-b0cd-a4802f0f10b6
```

Stop the service:

```console
sudo systemctl stop bpfd.service
```
