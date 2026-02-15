# Percy: a coding agent

Percy is a fork of [Shelley](https://github.com/boldsoftware/shelley) with some crazy cool enhancements.

Percy is a mobile-friendly, web-based, multi-conversation, multi-modal,
multi-model, single-user coding agent. It does not come with authorization or sandboxing:
bring your own.

*Mobile-friendly* because ideas can come any time.

*Web-based*, because terminal-based scroll back is punishment for shoplifting in some countries.

*Multi-modal* because screenshots, charts, and graphs are necessary, not to mention delightful.

*Multi-model* to benefit from all the innovation going on.

*Single-user* because it makes sense to bring the agent to the compute.

# Installation

## Pre-Built Binaries (macOS/Linux)

```bash
curl -Lo percy "https://github.com/tgruben-circuit/percy/releases/latest/download/percy_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" && chmod +x percy
```

The binaries are on the [releases page](https://github.com/tgruben-circuit/percy/releases/latest).

## Homebrew (macOS)

```bash
brew install --cask tgruben-circuit/tap/percy
```

## Build from Source

You'll need Go and Node.

```bash
git clone https://github.com/tgruben-circuit/percy.git
cd percy
make
```

# Releases

New releases are automatically created on every commit to `main`. Versions
follow the pattern `v0.N.9OCTAL` where N is the total commit count and 9OCTAL is the commit SHA encoded as octal (prefixed with 9).

# Architecture

The technical stack is Go for the backend, SQLite for storage, and Typescript
and React for the UI.

The data model is that Conversations have Messages, which might be from the
user, the model, the tools, or the harness. All of that is stored in the
database, and we use a SSE endpoint to keep the UI updated.

# History

Percy is a fork of [Shelley](https://github.com/boldsoftware/shelley), which was partially based on [Sketch](https://github.com/boldsoftware/sketch).

# Open source

Percy is Apache licensed. We require a CLA for contributions.

# Building Percy

Run `make`. Run `make serve` to start Percy locally.

## Dev Tricks

If you want to see how mobile looks, and you're on your home
network where you've got mDNS working fine, you can
run

```
socat TCP-LISTEN:9001,fork TCP:localhost:9000
```
