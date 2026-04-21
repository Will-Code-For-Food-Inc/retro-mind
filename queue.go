//go:build !slim

package main

import "path/filepath"

// WorkItem is a single game assignment returned to a tagging agent.
type WorkItem struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	CRC      string `json:"crc"`
}

// GetBatch returns the next batchSize untagged, not-in-progress games for
// a platform and atomically marks them as in-progress.
func GetBatch(platform string, batchSize int) ([]WorkItem, error) {
	files, err := ListROMFiles(romBase, platform)
	if err != nil {
		return nil, err
	}

	done, err := dbTaggedNames(platform)
	if err != nil {
		return nil, err
	}
	inProg, err := dbInProgress(platform)
	if err != nil {
		return nil, err
	}

	seenNames := make(map[string]struct{})
	var batch []WorkItem

	for _, filename := range files {
		if len(batch) >= batchSize {
			break
		}
		name := CleanROMName(filename)
		if name == "" {
			continue
		}
		if _, d := done[name]; d {
			continue
		}
		if _, p := inProg[name]; p {
			continue
		}
		if _, s := seenNames[name]; s {
			continue
		}
		seenNames[name] = struct{}{}

		crc, _ := FileCRC32(filepath.Join(romBase, platform, filename))
		batch = append(batch, WorkItem{Name: name, Filename: filename, CRC: crc})
	}

	if len(batch) > 0 {
		names := make([]string, len(batch))
		for i, item := range batch {
			names[i] = item.Name
		}
		if err := dbAddInProgress(platform, names); err != nil {
			return nil, err
		}
	}

	return batch, nil
}

// ResetQueue clears all in-progress entries for a platform.
func ResetQueue(platform string) {
	dbResetQueue(platform)
}
