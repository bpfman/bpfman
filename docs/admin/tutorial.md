# Tutorial

This tutorial will show you how to use bpfd.

## Prerequisites

This tutorial uses examples from the [xdp-tutorial](https://github.com/xdp-project/xdp-tutorial).
You will need to check out the git repository and compile the examples.

## Step 1: Start bpfd

``` console
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
1: 92e3e14c-0400-4a20-be2d-f701af21873c
        name: "xdp"
        priority: 100
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
2: 6af7c28f-6a7f-46ee-bc98-2d92ed261369
        name: "xdp"
        priority: 200
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
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
1: 6af7c28f-6a7f-46ee-bc98-2d92ed261369
        name: "xdp"
        priority: 200
        path: /home/dave/dev/xdp-tutorial/basic01-xdp-pass/xdp_pass_kern.o
```
