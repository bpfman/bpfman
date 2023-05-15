# Linux Capabilities

Linux divides the privileges traditionally associated with superuser into distinct units,
known as capabilities, which can be independently enabled and disabled.
Capabilities are a per-thread attribute.
See [capabilities man-page](https://man7.org/linux/man-pages/man7/capabilities.7.html).

When `bpfd` is run as a systemd service, the set of linux capabilities are restricted to only the
required set of capabilities via the `bpfd.service` file using the `AmbientCapabilities` and
`CapabilityBoundingSet` fields (see [bpfd.service](https://github.com/bpfd-dev/bpfd/tree/main/scripts/bpfd.service)).
All spawned threads are stripped of all capabilities, removing all sudo privileges
(see `drop_linux_capabilities()` usage), leaving only the main thread with only the needed set of capabilities.

## Debugging Linux Capabilities

As new features are added, the set of Linux capabilities required by bpfd may change over time.
The following describes the steps to determine the set of capabilities required by bpfd.
If there are any `Permission denied (os error 13)` type errors when starting or running bpfd as a
systemd service, adjusting the linux capabilities is a good place to start.

### Determine Required Capabilities

The first step is to turn all capabilities on and see if that fixes the problem.
This can be done without recompiling the code by editing `bpfd.service`.
Comment out the finite list of granted capabilities and set to `~`,  which indicates all capabilities.

```shell
sudo vi /usr/lib/systemd/system/bpfd.service
:
[Service]
:
AmbientCapabilities=~
CapabilityBoundingSet=~
#AmbientCapabilities=CAP_BPF CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_NET_ADMIN CAP_PERFMON CAP_SETPCAP CAP_SYS_ADMIN CAP_SYS_RESOURCE
#CapabilityBoundingSet=CAP_BPF CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_NET_ADMIN CAP_PERFMON CAP_SETPCAP CAP_SYS_ADMIN CAP_SYS_RESOURCE
```

Reload the service file and start/restart bpfd and watch the bpfd logs and see if the problem is resolved:

```shell
sudo systemctl daemon-reload
sudo systemctl start bpfd
```

If so, then the next step is to watch the set of capabilities being requested by bpfd.
Run the bcc `capable` tool to watch capabilities being requested real-time and restart bpfd:

```shell
$ sudo /usr/share/bcc/tools/capable
TIME      UID    PID    COMM             CAP  NAME                 AUDIT
:
16:36:00  979    75553  tokio-runtime-w  8    CAP_SETPCAP          1
16:36:00  979    75553  tokio-runtime-w  8    CAP_SETPCAP          1
16:36:00  979    75553  tokio-runtime-w  8    CAP_SETPCAP          1
16:36:00  0      616    systemd-journal  19   CAP_SYS_PTRACE       1
16:36:00  0      616    systemd-journal  19   CAP_SYS_PTRACE       1
16:36:00  979    75550  bpfd             24   CAP_SYS_RESOURCE     1
16:36:00  979    75550  bpfd             1    CAP_DAC_OVERRIDE     1
16:36:00  979    75550  bpfd             21   CAP_SYS_ADMIN        1
16:36:00  979    75550  bpfd             21   CAP_SYS_ADMIN        1
16:36:00  0      75555  modprobe         16   CAP_SYS_MODULE       1
16:36:00  0      628    systemd-udevd    2    CAP_DAC_READ_SEARCH  1
16:36:00  0      75556  bpf_preload      24   CAP_SYS_RESOURCE     1
16:36:00  0      75556  bpf_preload      39   CAP_BPF              1
16:36:00  0      75556  bpf_preload      39   CAP_BPF              1
16:36:00  0      75556  bpf_preload      39   CAP_BPF              1
16:36:00  0      75556  bpf_preload      38   CAP_PERFMON          1
16:36:00  0      75556  bpf_preload      38   CAP_PERFMON          1
16:36:00  0      75556  bpf_preload      38   CAP_PERFMON          1
:
```

Compare the output to list in `bpfd.service` and determine the delta.

### Determine Capabilities Per Thread

For additional debugging, it may be helpful to know the granted capabilities on a per thread basis.
As mentioned above, all spawned threads are stripped of all Linux capabilities, so if a thread is
requesting a capability, that functionality should be moved off the spawned thread and onto the main thread.

First, determine the `bpfd` process id, then determine the set of threads:

```shell
$ ps -ef | grep bpfd
:
bpfd       75550       1  0 16:36 ?        00:00:00 /usr/sbin/bpfd
:

$ ps -T -p 75550
    PID    SPID TTY          TIME CMD
  75550   75550 ?        00:00:00 bpfd
  75550   75551 ?        00:00:00 tokio-runtime-w
  75550   75552 ?        00:00:00 tokio-runtime-w
  75550   75553 ?        00:00:00 tokio-runtime-w
  75550   75554 ?        00:00:00 tokio-runtime-w
```

Then dump the capabilities of each thread:

```shel
$ grep Cap /proc/75550/status
CapInh: 000000c001201106
CapPrm: 000000c001201106
CapEff: 000000c001201106
CapBnd: 000000c001201106
CapAmb: 000000c001201106

$ grep Cap /proc/75551/status
CapInh: 0000000000000000
CapPrm: 0000000000000000
CapEff: 0000000000000000
CapBnd: 0000000000000000
CapAmb: 0000000000000000

$ grep Cap /proc/75552/status
CapInh: 0000000000000000
CapPrm: 0000000000000000
CapEff: 0000000000000000
CapBnd: 0000000000000000
CapAmb: 0000000000000000

:

$ capsh --decode=000000c001201106
0x000000c001201106=cap_dac_override,cap_dac_read_search,cap_setpcap,cap_net_admin,cap_sys_admin,cap_sys_resource,cap_perfmon,cap_bpf
```

## Removing CAP_BPF from bpfd Clients

One of the advantages of using bpfd is that it is doing all the loading and unloading of eBPF programs,
so it requires CAP_BPF, but clients of bpfd are just making gRPC calls to bpfd, so they do not need to
be privileged or require CAP_BPF.
It must be noted that this is only true for kernels 5.19 or higher.
Prior to **kernel 5.19**, all eBPF sys calls required CAP_BPF, which are used to access maps shared between
the BFP program and the userspace program.
In kernel 5.19, a change went in that only requires CAP_BPF for map creation (BPF_MAP_CREATE) and loading
programs (BPF_PROG_LOAD).
See [bpf: refine kernel.unprivileged_bpf_disabled behaviour](https://git.kernel.org/pub/scm/linux/kernel/git/bpf/bpf-next.git/commit/?id=c8644cd0efe7).
