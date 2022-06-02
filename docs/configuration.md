Configuration
=============

bpfd expects a configuration file to be present at `/etc/bpfd.toml`.
If no file is found, defaults are assumed.

```toml
[interfaces]
  [interface.eth0]
  xdp_mode = "hw" # Valid xdp modes are "hw", "skb" and "drv". Default: "skb".
```
