# Documentation

This section describes how to modify the related documentation around bpfman.
All bpfman's documentation is written in Markdown, and leverages [mkdocs](https://www.mkdocs.org/)
to generate a static site, which is hosted on [netlify](https://www.netlify.com/).

If this is the first time building using `mkdocs`, jump to the
[Development Environment Setup](#development-environment-setup) section for help installing
the tooling.

## Documentation Notes

This section describes some notes on the dos and don'ts when writing documentation.

### Website Management

The headings and layout of the website, as well as other configuration settings, are managed
from the [mkdocs.yml](https://github.com/bpfman/bpfman/blob/main/mkdocs.yml) file in the
project root directory.

### Markdown Style

When writing documentation via a Markdown file, the following format has been followed:

* Text on a given line should not exceed 100 characters, unless it's example syntax or a link
  that should be broken up.
* Each new sentence should start on a new line.
  That way, if text needs to be inserted, whole paragraphs don't need to be adjusted.
* Links to other markdown files are relative to the file the link is placed in.

### Governance Files

There are a set of well known governance files that are typically placed in the root directory
of most projects, like README.md, MAINTAINERS.md, CONTRIBUTING.md, etc.
`mkdocs` expects all files used in the static website to be located under a common directory,
`docs/` for bpfman.
To reference the governance files from the static website, a directory (`docs/governance/`) was
created with a file for each governance file, the only contains `--8<--` and the file name.
This indicates to `mkdocs` to pull the additional file from the project root directory.

For example: [docs/governance/MEETINGS.md](https://github.com/bpfman/bpfman/blob/main/docs/governance/MEETINGS.md)

> **NOTE:** This works for the website generation, but if a Markdown file is viewed through
  Github (not the website), the link is broken.
  So these files should only be linked from `docs/index.md` and `mkdocs.yml`.

### docs/developer-guide/api-spec.md

The file
[docs/developer-guide/api-spec.md](https://github.com/bpfman/bpfman/blob/main/docs/developer-guide/api-spec.md)
documents the CRDs used in a Kubernetes deployment.
The contents are auto-generated when PRs are pushed to Github.

The contents can be generated locally by running the command `make -C bpfman-operator apidocs.html` from the root bpfman directory.

## Generate Documentation

If you would like to test locally, build and preview the generated documentation,
from the bpfman root directory, use `mkdocs` to build:

```console
cd bpfman/
mkdocs build
```

>**NOTE:** If `mkdocs build` gives you an error, make sure you have the mkdocs
packages listed below installed.

To preview from a build on a local machine, start the mkdocs dev-server with the command below,
then open up `http://127.0.0.1:8000/` in your browser, and you'll see the default home page
being displayed:

```console
mkdocs serve
```

To preview from a build on a remote machine, start the mkdocs dev-server with the command below,
then open up `http://<ServerIP>:8000/` in your browser, and you'll see the default home page
being displayed:

```console
mkdocs serve -a 0.0.0.0:8000
```

## Development Environment Setup

The recommended installation method is using `pip`.

```console
pip install -r requirements.txt 
```

Once installed, ensure the `mkdocs` is in your PATH:

```console
mkdocs -V
mkdocs, version 1.4.3 from /home/$USER/.local/lib/python3.11/site-packages/mkdocs (Python 3.11)
```

>**NOTE:** If you have an older version of mkdocs installed, you may need to use
the `--upgrade` option (e.g., `pip install --upgrade mkdocs`) to get it to work.

## Document Images

Source of images used in the example documentation can be found in
[bpfman Upstream Images](https://docs.google.com/presentation/d/1wU4xu6xeyk9cB3G-Nn-dzkf90j1-EI4PB167G7v-Xl4/edit?usp=sharing).
Request access if required.
