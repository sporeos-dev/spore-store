---
description: "Use when locating Spore OS repositories, components, client libraries, nodes, specifications, installation tooling, or release artifacts."
---

# Repository Map
All potentially relevant repos of the Spore OS project.

---

## Meta Repos
#### Protocol
- found at ../spore-os-protocol
- spec documentation
- manifest schemas for validation
- historical releases

### Releases
- found at ../spore-os-releases
- artifacts for releases via github releases
- automation information

### Installation
- found at ../spore-install
- per-platform (macos, linux, windows) installation and building scripts
- per-platform (macos, linux, windows) directory and installation plan

---

## Code Repos
### Hub
- found at ../spore-os
- main hub of the spore-os ecosystem
- primary implementation of the spec

### Client Libraries
- found at ../spore-client-libs
- libraries to be brought into languages
  - parser: central parsing library used in both the hub and nodes
  - spore_c: central client library built in C++ under a C-interface to be used in other languages
  - spore_go: thin go wrapper

### Core Nodes: Shell, Spore, Log, Witness
- found at ../spore-core-nodes
- 4 nodes
  - shell (spore-shell): REPL, main terminal-based UI for spore
  - log (spore-log): logs events to a file
  - witness (spore-witness): live terminal display of logs
  - spore (spore): provides terminal/command-line access to spore commands

### Userspace Node
- found at ../spore-hyphae
- provides access to the core daemon in the userspace (because the core runs in the _spore space)

### Dialogs Node
- found at ../spore-dialog
- provides dialog boxes for things like alerts or path selection

### Store Node
- found at ../spore-store
- distribution channel for non-core and/or product nodes

### Smoke Testing
- found at ../spore-smoke
- 2 nodes
  - testing node: runs automated sanity/smoke tests
  - reporting node: creates a report out of the smoke testing

---

## Product Repos
- found at ../spore-git
- private repo
- an experiment to wrap git into the spore ecosystem
