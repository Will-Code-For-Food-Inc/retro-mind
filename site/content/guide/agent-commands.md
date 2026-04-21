---
title: Agent Tab Commands
weight: 2
---

# Agent Tab Commands

The agent tab uses a command-line-first DSL. Known commands are dispatched
deterministically; anything else is forwarded to the LLM as a chat prompt.

## Commands

### `tag <platform>`

Kick off the sequential vibe-check loop for a platform. The agent tags all
untagged games one at a time using the local LLM.

```
tag snes
tag n64
tag gba
```

### `query <game name>`

Look up a game by name.

```
query Axelay
query Super Mario World
```

### `normalize`

Run a tag normalization pass. Merges near-duplicate tags using semantic
similarity (BGE-M3 embeddings).

### `status`

Show tagging queue status — how many games are untagged per platform.

### `metrics`

Display LLM usage metrics from the local database (tokens, latency, model).

## Chat fallback

Any input that doesn't match a known command is forwarded to the LLM as a
free-form prompt. Use this for ad-hoc queries:

```
what are some good co-op games on snes?
which games have the "atmospheric" tag?
```
