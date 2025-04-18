# Contributing Guide

- [Contributing Guide](#contributing-guide)
  - [Ways to Contribute](#ways-to-contribute)
    - [Come to Meetings](#come-to-meetings)
  - [Find an Issue](#find-an-issue)
  - [Ask for Help](#ask-for-help)
  - [Pull Request Lifecycle](#pull-request-lifecycle)
  - [Development Environment Setup](#development-environment-setup)
  - [Signoff Your Commits](#signoff-your-commits)
    - [DCO](#dco)
  - [Logical Grouping of Commits](#logical-grouping-of-commits)
  - [Commit message guidelines](#commit-message-guidelines)
  - [Test Policy](#test-policy)
    - [Unit Tests](#unit-tests)
    - [Integration Tests](#integration-tests)
    - [End-to-End Tests](#end-to-end-tests)
    - [Test Coverage](#test-coverage)
    - [Running Tests Locally](#running-tests-locally)
  - [Pull Request Checklist](#pull-request-checklist)
    - [bpfman Pinned Rust Toolchain](#bpfman-pinned-rust-toolchain)
    - [bpfman Checklist](#bpfman-checklist)
    - [bpfman-operator Checklist](#bpfman-operator-checklist)

Welcome! We are glad that you want to contribute to our project! 💖

As you get started, you are in the best position to give us feedback on areas of
our project that we need help with including:

- Problems found during setting up a new developer environment
- Gaps in our Quickstart Guide or documentation
- Bugs in our automation scripts

If anything doesn't make sense, or doesn't work when you run it, please open a
bug report and let us know!

## Ways to Contribute

We welcome many different types of contributions including:

- New features
- Builds, CI/CD
- Bug fixes
- Documentation
- Issue Triage
- Answering questions on Slack/Mailing List
- Web design
- Communications / Social Media / Blog Posts
- Release management

Not everything happens through a GitHub pull request. Please come to our
[meetings](./MEETINGS.md) or [contact us](https://kubernetes.slack.com/archives/C04UJBW2553) and let's discuss how we can work
together.

### Come to Meetings

Absolutely everyone is welcome to come to any of our meetings. You never need an
invite to join us. In fact, we want you to join us, even if you don’t have
anything you feel like you want to contribute. Just being there is enough!

You can find out more about our meetings [here](./MEETINGS.md). You don’t have to turn on
your video. The first time you come, introducing yourself is more than enough.
Over time, we hope that you feel comfortable voicing your opinions, giving
feedback on others’ ideas, and even sharing your own ideas, and experiences.

## Find an Issue

We have good first issues for new contributors and help wanted issues suitable
for any contributor. [good first issue](https://github.com/bpfman/bpfman/labels/good%20first%20issue) has extra information to
help you make your first contribution. [help wanted](https://github.com/bpfman/bpfman/labels/help%20wanted) are issues
suitable for someone who isn't a core maintainer and is good to move onto after
your first pull request.

Sometimes there won’t be any issues with these labels. That’s ok! There is
likely still something for you to work on. If you want to contribute but you
don’t know where to start or can't find a suitable issue, you can reach out to us on Slack and we will be happy to help.

Once you see an issue that you'd like to work on, please post a comment saying
that you want to work on it. Something like "I want to work on this" is fine.

## Ask for Help

The best way to reach us with a question when contributing is to ask on:

- The original github issue
- Our Slack channel

## Pull Request Lifecycle

Pull requests are managed by Mergify.

Our process is currently as follows:

1. When you open a PR a maintainer will automatically be assigned for review
1. Make sure that your PR is passing CI - if you need help with failing checks please feel free to ask!
1. Once it is passing all CI checks, a maintainer will review your PR and you may be asked to make changes.
1. When you have received at least one approval from a maintainer, your PR will be merged automatically.

In some cases, other changes may conflict with your PR. If this happens, you will get notified by a comment in the issue that your PR requires a rebase, and the `needs-rebase` label will be applied. Once a rebase has been performed, this label will be automatically removed.

## Development Environment Setup

See [Setup and Building bpfman](https://bpfman.io/main/getting-started/building-bpfman/#development-environment-setup)

## Signoff Your Commits

### DCO

Licensing is important to open source projects. It provides some assurances that
the software will continue to be available based under the terms that the
author(s) desired. We require that contributors sign off on commits submitted to
our project's repositories. The [Developer Certificate of Origin
(DCO)](https://probot.github.io/apps/dco/) is a way to certify that you wrote and
have the right to contribute the code you are submitting to the project.

You sign-off by adding the following to your commit messages. Your sign-off must
match the git user and email associated with the commit.

    This is my commit message

    Signed-off-by: Your Name <your.name@example.com>

Git has a `-s` command line option to do this automatically:

    git commit -s -m 'This is my commit message'

If you forgot to do this and have not yet pushed your changes to the remote
repository, you can amend your commit with the sign-off by running

    git commit --amend -s 

## Logical Grouping of Commits

It is a recommended best practice to keep your changes as logically grouped as
possible within individual commits. If while you're developing you prefer doing
a number of commits that are "checkpoints" and don't represent a single logical
change, please squash those together before asking for a review.
When addressing review comments, please perform an interactive rebase and edit commits directly rather than adding new commits with messages like "Fix review comments".

## Commit message guidelines

A good commit message should describe what changed and why.

1. The first line should:
  
- contain a short description of the change (preferably 50 characters or less,
    and no more than 72 characters)
- be entirely in lowercase with the exception of proper nouns, acronyms, and
    the words that refer to code, like function/variable names
- be prefixed with the name of the sub crate being changed

  Examples:

  - bpfman: validate program section names
  - bpf: add dispatcher program test slot

2. Keep the second line blank.
3. Wrap all other lines at 72 columns (except for long URLs).
4. If your patch fixes an open issue, you can add a reference to it at the end
   of the log. Use the `Fixes: #` prefix and the issue number. For other
   references use `Refs: #`. `Refs` may include multiple issues, separated by a
   comma.

   Examples:

   - `Fixes: #1337`
   - `Refs: #1234`

Sample complete commit message:

```txt
subcrate: explain the commit in one line

Body of commit message is a few lines of text, explaining things
in more detail, possibly giving some background about the issue
being fixed, etc.

The body of the commit message can be several paragraphs, and
please do proper word-wrap and keep columns shorter than about
72 characters or so. That way, `git log` will show things
nicely even when it is indented.

Fixes: #1337
Refs: #453, #154
```

## Test Policy

Testing is a critical part of the development process. All contributions must include tests to verify the functionality of the code. This ensures that the code works as expected and helps prevent future regressions.

### Unit Tests

Unit tests should cover individual components or functions. They should be fast and isolated from external dependencies.

### Integration Tests

Integration tests should verify that different components work together as expected. These tests may involve external dependencies and should be run in an environment that closely resembles production.

### End-to-End Tests

End-to-end tests should simulate real-world scenarios to ensure the entire system works as expected. These tests are typically slower and more complex than unit or integration tests.

### Test Coverage

We aim for high test coverage across the codebase. While 100% coverage is not always feasible, contributors should strive to cover as much of their code as possible.

### Running Tests Locally

Before submitting a pull request, contributors should run all tests locally to ensure they pass. This includes unit, integration, and end-to-end tests.

## Pull Request Checklist

When you submit your pull request, or you push new commits to it, our automated
systems will run some checks on your new code. We require that your pull request
passes these checks, but we also have more criteria than just that before we can
accept and merge it. We recommend that you check the following things locally
before you submit your code.

### bpfman Pinned Rust Toolchain

bpfman is coded in Rust and uses the latest `nightly` Rust toolchain for some of
the tools.
There are periods where the Rust toolchain may be pinned to a fixed version due to
tool issues.
Examine
[bpfman/.github/workflows/build.yml](https://github.com/bpfman/bpfman/blob/main/.github/workflows/build.yml)
and find `NIGHTLY_VERSION` to determine if the nightly toolchain is currently pinned.

- Example of using latest toolchain: `NIGHTLY_VERSION: nightly`
- Example of using pinned toolchain: `NIGHTLY_VERSION: nightly-2024-09-24`

If the toolchain is pinned, use the following to install a pinned toolchain then show all the
installed toolchains:

```console
rustup toolchain install nightly-2024-09-24

rustup show -v
```

Then replace `+nightly` in the commands below with the pinned toolchain
`+nightly-2024-09-24`.

### bpfman Checklist

Before submitting a pull request to the bpfman repository, verify the following:

- Verify that Rust code has been formatted and that all clippy lints have been fixed:

    ```console
    cd bpfman/
    cargo +nightly clippy --all -- --deny warnings
    ```

- Verify that the code has been formatted and linted:

    ```console
    cargo +nightly fmt --all -- --check
    ```

- Verify that Yaml files have been formatted (see
  [Install Yaml Formatter](https://bpfman.io/main/getting-started/building-bpfman/#install-yaml-formatter))

    ```console
    prettier -l "*.yaml"
    ```

- Verify that Bash scripts have been linted using `shellcheck`

    ```console
    cargo xtask lint
    ```

- Verify that unit tests are passing locally (see
  [Unit Testing](https://bpfman.io/main/developer-guide/testing/#unit-testing)):

    ```console
    cargo xtask unit-test
    ```

- Verify that integration tests are passing locally (see
  [Basic Integration Tests](https://bpfman.io/main/developer-guide/testing/#basic-integration-tests)):

    ```console
    cargo xtask integration-test
    ```

### bpfman-operator Checklist

- If developing the bpfman-operator, verify that bpfman-operator unit and integration tests
  are passing locally:
  
    See [Kubernetes Operator Tests](https://bpfman.io/main/developer-guide/testing/#kubernetes-operator-tests).
