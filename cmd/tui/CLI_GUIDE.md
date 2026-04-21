# rom-tagger TUI / CLI Guide

## Running It

From the repo root:

```bash
./rom-tagger/cmd/tui/rom-tagger-tui
```

If the TUI cannot find the backend binary automatically:

```bash
ROM_TAGGER_MCP_BIN=./rom-tagger/rom-tagger ./rom-tagger/cmd/tui/rom-tagger-tui
```

If the database is not in the default location:

```bash
ROM_TAGGER_DATA_DIR=/path/to/data ./rom-tagger/cmd/tui/rom-tagger-tui
```

If playlist emission needs a ROM base path:

```bash
ROM_TAGGER_ROM_PATH=/data/nfs/roms ./rom-tagger/cmd/tui/rom-tagger-tui
```

## Navigation

- `1` games
- `2` tags
- `3` playlists
- `4` tools
- `Tab` cycle views
- `/` search or filter current view
- `Enter` open selected item
- `Esc` back or clear filter
- `q` quit

## Browser Views

### Games

- browse all games
- `Enter` opens the game detail view

### Tags

- browse tags and counts
- `Enter` drills into games for the selected tag

### Playlists

- browse playlists
- `Enter` opens the playlist contents
- inside a playlist, `e` opens export format selection

### Tools

- browse the MCP-style tool catalog
- `Enter` prefills the selected tool into command mode
- recent command results are shown in the lower pane

## Command Mode

Press `:` to open the command prompt.

The TUI command prompt calls the same tool surface as the `rom-tagger` MCP/stdin server. The goal is human-friendly access to the existing tools, not a separate implementation.

## Command Syntax

### Positional Arguments

Use positional arguments for simple required fields:

```text
check_tag cozy
get_game "Golden Axe"
get_game_by_crc 74C65A49
```

### Named Arguments

Use `name=value` when a tool has several arguments:

```text
list_games platform=gba
similar_games name="Golden Axe" limit=5
emit_playlist name="Chill SMS Games" format=pegasus
```

### Arrays

Use square brackets for array arguments:

```text
tags=[cozy,arcade,"short session"]
crcs=[74C65A49]
games=["Golden Axe","Out Run"]
```

## Common Examples

```text
list_tags
check_tag cozy
add_tag "short session"
list_games
list_games platform=genesis
get_game "Golden Axe"
get_game_by_crc 74C65A49
clean_rom_name "Golden Axe (USA, Europe).zip"
fetch_game_metadata "Golden Axe"
set_game_tags name="Golden Axe" platform=genesis tags=[arcade,"beat em up"] crcs=[74C65A49]
create_playlist name="Chill SMS Games" description="good evening wind-down set" games=["Sonic Chaos","Columns"]
list_playlists
get_playlist "Chill SMS Games"
emit_playlist name="Chill SMS Games" format=pegasus
similar_games name="Golden Axe" limit=5
game_vibes name="Golden Axe" limit=8
```

## Parser Notes

- double quotes and single quotes are supported
- bare words are treated as strings unless they parse as numbers or booleans
- booleans are `true` and `false`
- arrays are flat
- spaces inside tags are preserved; they are not normalized to dashes

## Environment Variables

- `ROM_TAGGER_MCP_BIN`: explicit path to the backend `rom-tagger` binary
- `ROM_TAGGER_DATA_DIR`: location of `rom-tagger.db`
- `ROM_TAGGER_ROM_PATH`: ROM root used by playlist-emission flows
