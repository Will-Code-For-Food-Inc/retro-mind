//go:build !slim

package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
)

// EmitFormat is a playlist output driver.
type EmitFormat struct {
	Name     string
	Filename string // e.g. "metadata.pegasus.txt"
	Emit     func(name, description string, games []db.GameEntry, romBase string) string
}

var Formats = map[string]EmitFormat{
	"pegasus": {
		Name:     "pegasus",
		Filename: "metadata.pegasus.txt",
		Emit:     EmitPegasus,
	},
	"m3u": {
		Name:     "m3u",
		Filename: "",
		Emit:     EmitM3U,
	},
	"retroarch": {
		Name:     "retroarch",
		Filename: "",
		Emit:     EmitRetroArch,
	},
	"emulationstation": {
		Name:     "emulationstation",
		Filename: "gamelist.xml",
		Emit:     EmitEmulationStation,
	},
}

func EmitPegasus(name, description string, games []db.GameEntry, romBase string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "collection: %s\n", name)
	if description != "" {
		fmt.Fprintf(&b, "summary: %s\n", description)
	}
	b.WriteString("\n")

	for _, g := range games {
		fmt.Fprintf(&b, "game: %s\n", g.Name)
		// Find the ROM file on disk
		romPath := FindROM(romBase, g.Platform, g.Name)
		if romPath != "" {
			fmt.Fprintf(&b, "file: %s\n", romPath)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func EmitM3U(name, description string, games []db.GameEntry, romBase string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n")
	if description != "" {
		fmt.Fprintf(&b, "# %s\n", description)
	}
	for _, g := range games {
		romPath := FindROM(romBase, g.Platform, g.Name)
		if romPath != "" {
			fmt.Fprintf(&b, "#EXTINF:-1,%s\n%s\n", g.Name, romPath)
		}
	}
	return b.String()
}

// retroarchLPLEntry is the JSON structure for a RetroArch .lpl playlist item.
type retroarchLPLEntry struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	CorePath string `json:"core_path"`
	CoreName string `json:"core_name"`
	CRC32    string `json:"crc32"`
	DBName   string `json:"db_name"`
}

type retroarchLPL struct {
	Version         string              `json:"version"`
	DefaultCorePath string              `json:"default_core_path"`
	DefaultCoreName string              `json:"default_core_name"`
	Items           []retroarchLPLEntry `json:"items"`
}

func EmitRetroArch(name, description string, games []db.GameEntry, romBase string) string {
	lpl := retroarchLPL{Version: "1.5"}
	for _, g := range games {
		romPath := FindROM(romBase, g.Platform, g.Name)
		if romPath == "" {
			continue
		}
		crc := ""
		if len(g.CRCs) > 0 {
			crc = g.CRCs[0]
		}
		lpl.Items = append(lpl.Items, retroarchLPLEntry{
			Path:  romPath,
			Label: g.Name,
			CRC32: crc,
		})
	}
	data, _ := json.MarshalIndent(lpl, "", "  ")
	return string(data) + "\n"
}

func EmitEmulationStation(name, description string, games []db.GameEntry, romBase string) string {
	type esGame struct {
		XMLName xml.Name `xml:"game"`
		Path    string   `xml:"path"`
		Name    string   `xml:"name"`
		Desc    string   `xml:"desc,omitempty"`
	}
	type esGameList struct {
		XMLName xml.Name `xml:"gameList"`
		Games   []esGame `xml:"game"`
	}

	gl := esGameList{}
	for _, g := range games {
		romPath := FindROM(romBase, g.Platform, g.Name)
		if romPath == "" {
			continue
		}
		desc := strings.Join(g.Tags, ", ")
		gl.Games = append(gl.Games, esGame{Path: romPath, Name: g.Name, Desc: desc})
	}

	header := xml.Header
	data, _ := xml.MarshalIndent(gl, "", "  ")
	return header + string(data) + "\n"
}

// FindROM searches for a ROM file matching a game name in romBase/platform/.
// Returns the full path if found, empty string otherwise.
func FindROM(romBase, platform, gameName string) string {
	dir := filepath.Join(romBase, platform)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		cleaned := CleanROMName(e.Name())
		if strings.EqualFold(cleaned, gameName) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// EmitPlaylist writes a playlist file to disk in the given format.
// Returns the output path and file contents.
func EmitPlaylist(romBase, playlistName, format, outDir string) (string, string, error) {
	f, ok := Formats[format]
	if !ok {
		avail := make([]string, 0, len(Formats))
		for k := range Formats {
			avail = append(avail, k)
		}
		return "", "", fmt.Errorf("unknown format %q (available: %s)", format, strings.Join(avail, ", "))
	}

	entry, games, err := db.GetPlaylist(playlistName)
	if err != nil {
		return "", "", err
	}
	if entry == nil {
		return "", "", fmt.Errorf("playlist %q not found", playlistName)
	}

	content := f.Emit(entry.Name, entry.Description, games, romBase)

	// Determine output path
	slug := strings.ReplaceAll(strings.ToLower(entry.Name), " ", "-")
	var outPath string
	switch format {
	case "pegasus", "emulationstation":
		// Directory-based: outDir/playlist-slug/filename
		dir := filepath.Join(outDir, slug)
		os.MkdirAll(dir, 0755)
		outPath = filepath.Join(dir, f.Filename)
	case "retroarch":
		os.MkdirAll(outDir, 0755)
		outPath = filepath.Join(outDir, slug+".lpl")
	default:
		os.MkdirAll(outDir, 0755)
		outPath = filepath.Join(outDir, slug+".m3u")
	}

	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", outPath, err)
	}

	return outPath, content, nil
}
