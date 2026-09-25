// Package presets is the built-in session library, made from the example
// sessions in example_config.
package presets

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	exampleconfig "github.com/Wundark/binaural-beats/example_config"
	"github.com/Wundark/binaural-beats/internal/engine"
)

// Preset describes one built-in session.
type Preset struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	TotalDuration float64 `json:"total_duration"`
}

// List returns the built-in sessions, shortest first.
func List() ([]Preset, error) {
	files, err := fs.Glob(exampleconfig.FS, "*.yaml")
	if err != nil {
		return nil, err
	}
	var list []Preset
	for _, f := range files {
		id := strings.TrimSuffix(f, path.Ext(f))
		data, err := exampleconfig.FS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		cfg, err := engine.ParseConfigData(f, data)
		if err != nil {
			return nil, fmt.Errorf("preset %s: %w", id, err)
		}
		changes := cfg.FrequencyChanges
		list = append(list, Preset{
			ID:            id,
			Name:          cfg.Name,
			Description:   cfg.Description,
			TotalDuration: changes[len(changes)-1].Time,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].TotalDuration != list[j].TotalDuration {
			return list[i].TotalDuration < list[j].TotalDuration
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

// Load loads the built-in session id into eng.
func Load(eng *engine.Engine, id string) (engine.SessionInfo, error) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return engine.SessionInfo{}, fmt.Errorf("unknown preset %q", id)
	}
	data, err := exampleconfig.FS.ReadFile(id + ".yaml")
	if err != nil {
		return engine.SessionInfo{}, fmt.Errorf("unknown preset %q", id)
	}
	return eng.LoadConfigData(id+".yaml", data)
}
