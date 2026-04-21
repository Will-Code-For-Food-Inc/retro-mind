package core

import (
	"archive/zip"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ROM filename cleaning — applied in order.
// ROM naming conventions put all metadata (region, version, flags) in
// parentheses/brackets after the title, so stripping all of them is safe.
var (
	reArchiveSuffix = regexp.MustCompile(`#.*$`)
	reExtension     = regexp.MustCompile(`\.[a-zA-Z0-9]{1,5}$`)
	reBrackets      = regexp.MustCompile(`\s*\[[^\]]*\]`)
	reParens        = regexp.MustCompile(`\s*\([^)]*\)`)
	reTrailing      = regexp.MustCompile(`[\s\-_,]+$`)
)

// CleanROMName converts a raw ROM filename (or archive path like file.zip#rom.gba)
// into a canonical human-readable game title.
//
//	"Golden Axe (USA, Europe).zip"                    → "Golden Axe"
//	"Castlevania - Circle of the Moon (U) [f1].gba"  → "Castlevania - Circle of the Moon"
//	"Pokemon - Ruby Version (U) (V1.0) [!].gba"      → "Pokemon - Ruby Version"
func CleanROMName(filename string) string {
	name := filepath.Base(filename)
	name = reArchiveSuffix.ReplaceAllString(name, "")
	name = reExtension.ReplaceAllString(name, "")
	// Strip brackets before parens so things like "Title [hack] (U)" work correctly
	name = reBrackets.ReplaceAllString(name, "")
	name = reParens.ReplaceAllString(name, "")
	name = reTrailing.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

// ListROMFiles returns bare filenames (not full paths) for all non-directory
// entries under <romBase>/<platform>/.
func ListROMFiles(romBase, platform string) ([]string, error) {
	dir := filepath.Join(romBase, platform)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// FileCRC32 computes the CRC32 (uppercase hex) of a ROM file.
// For ZIP archives it reads the CRC stored in the ZIP central directory
// for the first contained file — no decompression needed.
// For all other files it streams the content through a CRC32 hasher.
func FileCRC32(path string) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return zipInnerCRC32(path)
	}
	return streamCRC32(path)
}

func zipInnerCRC32(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("opening zip %s: %w", path, err)
	}
	defer r.Close()
	if len(r.File) == 0 {
		return "", fmt.Errorf("empty zip: %s", path)
	}
	return fmt.Sprintf("%08X", r.File[0].CRC32), nil
}

func streamCRC32(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08X", h.Sum32()), nil
}
