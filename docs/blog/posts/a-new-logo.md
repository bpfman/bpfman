---
date: 2023-11-25
authors:
  - dave-tucker
---

# A New Logo: Using Generative AI, of course

Since we renamed the project to `bpfman` we are in need of a new logo.
Given that the tech buzz around Generative AI is infection, we decided to use
generative AI to create our new logo.

<!-- more -->

## The Brief

I have a love of open source projects with animal mascots, so bpfman
should be no different. The "bee" is used a lot for eBPF related projects.
One such example is [Crabby], the crab/bee hybrid, that I created for the
[Aya] project.

The logo should be cute and playful, but not too childish.
As a nod to [Podman], we'd like to use the same typeface and split color-scheme
as they do, replacing purple with yellow.

One bee is not enough! Since we're an eBPF manager, we need a more bees!

<iframe src="https://giphy.com/embed/QBYeMohXoVUJBtlfFD" width="480" height="276" frameBorder="0" class="giphy-embed" allowFullScreen></iframe><p><a href="https://giphy.com/gifs/teamcoco-oprah-bees-QBYeMohXoVUJBtlfFD">via GIPHY</a></p>

And since those bees are bee-ing (sorry) managed, they should be organized.
Maybe in a pyramid shape?

[Aya]: https://aya-rs.dev
[Crabby]: https://github.com/crabby-the-crab
[Podman]: https://podman.io

## The Process

We used [Bing Image Creator](https://www.bing.com/images/create/), which is
backed by [DALL-E 3](https://www.microsoft.com/en-us/bing/do-more-with-ai/image-creator-improvements-dall-e-3).

Initially we tried to use the following prompt:

> Logo for open source software project called "bpfman". "bpf" should be yellow
> and "man" should be black or grey. an illustration of some organized bees
> above the text. cute. playful

Our AI overlords came up with:

![first attempt](./img/2021-11-25/bpfman-logo-1.png)

Not bad, but not quite what we were looking for. It's clear that as smart as
AI is, it struggles with text, so whatever we need will need some manual
post-processing. There are bees, if you squint a bit, but they're not very
organized. Let's refine our prompt a bit:

> Logo for open source software project called "bpfman" as one word.
> The "bpf" should be yellow and "man" should be black or grey.
> an illustration of some organized bees above the text. cute. playful.

![second attempt](./img/2021-11-25/bpfman-logo-2.png)

That... is worse.

Let's try again:

> Logo for a project called "bpfman".
> In the text "bpfman", "bpf" should be yellow and "man" should be black or grey.
> add an illustration of some organized bees above the text.
> cute and playful style.

![third attempt](./img/2021-11-25/bpfman-logo-3.png)

The bottom left one is pretty good! So I shared it with the rest of the
maintainers to see what they thought.

At this point the feedback that I got was the bees were too cute!
We're a manager, and managers are serious business, so we need serious bees.

Prompting the AI for the whole logo was far too ambitious, so I decided I would
just use the AI to generate the bees and then I would add the text myself.

I tried a few different prompts, but the one that worked best was:

> 3 bees guarding a hive. stern expressions. simple vector style.

![fourth attempt](./img/2021-11-25/bpfman-logo-4.png)

The bottom right was exactly what I was looking for!

## The Result

I took the bottom right image, converted it to SVG, removed the background,
and added the text.

Here's the final result:

![final result](./img/2021-11-25/bpfman-logo-final.png)

## Conclusion

I'm really happy with the result! It was significantly easier than trying to
draw the bees myself, and I think it looks great! What do you think?

We're not quite ready to replace the logo on the website yet, but we will soon!
So if you have opinions, now is the time to share them on the [Slack].

[Slack]: https://kubernetes.slack.com/archives/C04UJBW2553
