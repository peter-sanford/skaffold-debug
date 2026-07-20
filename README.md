<!-- github does not support `width` with markdown images-->
<img src="logo/skaffold.png" width="220">

---------------------

[![Code Coverage](https://codecov.io/gh/GoogleContainerTools/skaffold/branch/main/graph/badge.svg)](https://codecov.io/gh/GoogleContainerTools/skaffold)
[![LICENSE](https://img.shields.io/github/license/GoogleContainerTools/skaffold.svg)](https://github.com/GoogleContainerTools/skaffold/blob/main/LICENSE)
[![Releases](https://img.shields.io/github/release-pre/GoogleContainerTools/skaffold.svg)](https://github.com/GoogleContainerTools/skaffold/releases)

Skaffold is a command line tool that facilitates continuous development for
Kubernetes applications. You can iterate on your application source code
locally then deploy to local or remote Kubernetes clusters. Skaffold handles
the workflow for building, pushing and deploying your application. It also
provides building blocks and describe customizations for a CI/CD pipeline.

---------------------

## `cachedebug` branch: cache-debug fork

This repo is a personal fork of [GoogleContainerTools/skaffold](https://github.com/GoogleContainerTools/skaffold).

- `main` tracks upstream `main` as a clean mirror (no local changes) — it's rebased/fast-forwarded from
  `upstream/main` periodically and never carries the patch directly.
- `cachedebug` is rebased on top of `main` and adds two things:
  1. `pkg/skaffold/build/cache/hash.go` gains `CACHEDEBUG` logging (written to **stderr**) for every input
     that feeds an artifact's build-cache hash — per-dependency file hashes, build args, artifact config,
     resolved platforms, and the final combined hash. Useful for diagnosing why `skaffold build`/`dev`
     decides an artifact's cache is a hit or a miss. **Off by default** (one line per dependency file
     per artifact per build is a lot of noise) — set `SKAFFOLD_CACHEDEBUG=1` to turn it on.
  2. `pkg/skaffold/build/cache/cachedebug.go` snapshots those same inputs to
     `~/.skaffold/cache-deps/<artifact>.txt` on every run (tab-separated `key\tvalue` lines, one per
     dependency file plus a few `!meta! ...`-prefixed entries for config/build args/platforms), diffs the
     snapshot against the previous run, and — if anything differs — follows the normal
     `Not found. Building` line **on stdout** with every file that changed/was added/was removed
     (`~`/`+`/`-` respectively), one per line, no truncation:
     ```
      - portal: Not found. Building
         changes:
           ~ app.py
           + new-file.txt
           - old-config.ini
     ```
     Silent when nothing changed (normal cache hits stay quiet), and printed on stdout
     specifically so it's visible even with stderr redirected to `/dev/null`. **Always on**,
     unlike the raw `CACHEDEBUG` lines above — this is what makes a plain build actionable
     without turning on verbose logging first.

     If it's `Not found. Building` with *no* `changes:` block, that doesn't mean nothing
     changed — it means none of *this artifact's own* tracked inputs changed, and one of three
     things is going on instead (a one-line `note:` explains which):
     - **first tracked run** for this artifact — no previous snapshot to diff against yet
     - **a required/dependent artifact changed** — the combined hash used for the cache lookup
       folds in every artifact this one `requires:`, but cache-deps only snapshots this
       artifact's own direct inputs, so a change purely in a required artifact shows up as a
       miss here with nothing to point at
     - **the previously-built image is gone** — the hash matched a real prior entry in
       `~/.skaffold/cache`, but that image no longer resolves locally or remotely (pruned,
       evicted, `docker system prune`, etc.) — rebuilding just recreates it, nothing to fix

     Override the snapshot directory with
     `SKAFFOLD_CACHE_DEBUG_DIR` — the default is deliberately *not*
     relative to the working directory, since most artifacts' `dependencies.paths` include `.` or
     the project root, and a snapshot file living inside that tree would count itself as a
     changed dependency on every subsequent run.

### Pulling in upstream changes

```sh
git fetch upstream
git checkout main
git merge --ff-only upstream/main   # main should always fast-forward cleanly
git checkout cachedebug
git rebase main
```

### Building the debug binary

```sh
git checkout cachedebug
make out/skaffold
cp out/skaffold ~/Documents/skaffold-cache-debug
```

`out/skaffold` is the plain dev build target (see `Makefile`) — no cross-compilation, just your host's
`GOOS`/`GOARCH`. The `CACHEDEBUG` lines print to stderr, so capture both streams when redirecting output:

```sh
skaffold-cache-debug build -f skaffold.yaml > output.txt 2>&1
```

---------------------

## [Install Skaffold](https://skaffold.dev/docs/install/)

Or, check out our [Github Releases](https://github.com/GoogleContainerTools/skaffold/releases) page for release info or to install a specific version.

![Demo](docs/static/images/intro.gif)

## Features

* Blazing fast local development
  * **optimized source-to-deploy** - Skaffold detects changes in your source code and handles the pipeline to
  **build**, **push**, and **deploy** your application automatically with **policy based image tagging**
  * **continuous feedback** - Skaffold automatically aggregates logs from deployed resources and forwards container ports to your local machine
* Project portability
  * **share with other developers** - Skaffold is the easiest way to **share your project** with the world: `git clone` and `skaffold run`
  * **context aware** - use Skaffold profiles, user level config, environment variables and flags to describe differences in environments
  * **CI/CD building blocks** - use `skaffold run` end-to-end, or use individual Skaffold phases to build up your CI/CD pipeline. `skaffold render` outputs hydrated Kubernetes manifests that can be used in GitOps workflows.
* Pluggable, declarative configuration for your project
  * **skaffold init** - Skaffold discovers your files and generates its own config file
  * **multi-component apps** - Skaffold supports applications consisting of multiple components
  * **bring your own tools** - Skaffold has a pluggable architecture to integrate with any build or deploy tool
* Lightweight
  * **client-side only** - Skaffold has no cluster-side component, so there is no overhead or maintenance burden
  * **minimal pipeline** - Skaffold provides an opinionated, minimal pipeline to keep things simple

### Check out our [examples page](./examples) for more complex workflows!

## IDE integrations

For a managed experience of Skaffold you can install the Google `Cloud Code` extensions:
- for [Visual Studio Code](https://cloud.google.com/code/docs/vscode/quickstart-k8s#installing)
- for [JetBrains IDEs](https://cloud.google.com/code/docs/intellij/quickstart-k8s#installing_the_plugin). 

It can manage and keep Skaffold  up-to-date while providing a more guided startup experience, along with providing and managing other common dependencies, and works with any kubernetes cluster. 

## Contributing to Skaffold

We welcome any contributions from the community with open arms - Skaffold wouldn't be where it is today without contributions from the community! Have a look at our [contribution guide](./CONTRIBUTING.md) for more information on how to get started on sending your first PR.

## Community

* [#skaffold on Kubernetes Slack](https://kubernetes.slack.com/messages/CABQMSZA6/)
* [skaffold-users mailing list](https://groups.google.com/forum/#!forum/skaffold-users)

## Support 

Skaffold is generally available and considered production ready.
Detailed feature maturity information and how we deprecate features are described in our [Deprecation Policy](https://skaffold.dev/docs/references/deprecation).

## Security Disclosures

Please see our [security disclosure process](SECURITY.md).  All [security advisories](https://github.com/GoogleContainerTools/skaffold/security/advisories) are managed on Github.
