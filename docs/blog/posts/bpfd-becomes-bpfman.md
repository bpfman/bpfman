---
date: 2023-11-23
authors:
  - dave-tucker
---

# bpfd becomes bpfman

Bpfd is now bpfman! We've renamed the project to better reflect the
direction we're taking. We're still the same project, just with a new
name.

<!-- more -->

## Why the name change?

We've been using the name `bpfd` for a while now, but we were not the first to
use it. There were projects before us that used the name `bpfd`, but since most
were inactive, originally we didn't see this as an issue.

More recently though the folks at [Meta] have started using the name
`systemd-bpfd` for their proposed addition to [systemd].

In addition, we've been thinking about the future of the project, and
particularly about security and whether it's wise to keep something with
`CAP_BPF` capabilities running as a daemon - even if we've been very careful.
This is similar to the [issues faced by docker](https://docs.docker.com/engine/security/#docker-daemon-attack-surface) which eventually lead to the creation of podman.

This [issue](https://github.com/bpfman/bpfd/issues/693) led us down
the path of redesigning the project to be daemonless. We'll be
implementing these changes in the coming months and plan to perform
our first release as `bpfman` in Q1 of 2024.

The 'd' in `bpfd` stood for daemon, so with our new design and the
confusion surrounding the name `bpfd` we though it was time for a change.

Since we're a BPF manager, we're now bpfman!
It's also a nice homage to [podman](https://podman.io/), which we're big fans of.

## What does this mean for me?

If you're a developer of `bpfman` you will need to update your Git remotes
to point at our new organization and repository name. Github will redirect
these for a while, but we recommend updating your remotes as soon as possible.

If you're a user of `bpfd` or the `bpfd-operator` then version 0.3.1 will be
the last release under the `bpfd` name. We will continue to support you as best
we can, but we recommend upgrading to `bpfman` as soon as our first release is
available.

## What's next?

We've hinted at some of the changes we're planning, and of course, our
roadmap is always available in [Github]. It's worth mentioning that we're
also planning to expand our release packages to include RPMs and DEBs, making it
even easier to install `bpfman` on your favorite Linux distribution.

## Thanks!

We'd like to thank everyone who has contributed to `bpfd` over the years.
We're excited about the future of `bpfman` and we hope you are too!
Please bear with us as we make this transition, and if you have any questions
or concerns, please reach out to us on [Slack](https://slack.k8s.io/).
We're in the '#bpfd' channel, but we'll be changing that to '#bpfman' soon.

[Github]: https://github.com/orgs/bpfman/projects/4/views/2
[Meta]: https://meta.com/
[systemd]: https://systemd.io/
