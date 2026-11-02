// Package store holds MonoModule's files: its settings, the project and the
// UserWave samples. Plain Go, tested without hardware.
package store

import (
	"encoding/json"
	"monomodule/outmix"
	"os"
	"path/filepath"
)

// ConfigFile sits next to hack.json and survives updates.
const ConfigFile = "monomodule-config.json"

// Config is what the user can change from the web page or by hand.
type Config struct {
	// PCMDevice is the loopback side MonoModule writes to.
	PCMDevice string `json:"pcm_device"`
	// OutputMode is "stereo" (a pair of channels) or "mono" (one channel).
	OutputMode string `json:"output_mode"`
	// ChannelOffset is the first channel (0-based) on the card: stereo 2 is
	// channels 3/4, mono 2 is channel 3.
	ChannelOffset int `json:"channel_offset"`
	// MidiChannel is the base channel (0-15): track n listens on base+n-1. -1 means
	// the default, channel 1.
	MidiChannel int `json:"midi_channel"`
	// PushManagerURL is for development only.
	PushManagerURL string `json:"push_manager_url,omitempty"`
}

// DefaultConfig is a fresh install.
func DefaultConfig() Config {
	return Config{PCMDevice: "hw:PHVAudio,1,0", OutputMode: outmix.Stereo, MidiChannel: -1}
}

// LoadConfig reads the config; a missing file is the defaults.
func LoadConfig(dir string) (Config, error) {
	c := DefaultConfig()
	b, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return DefaultConfig(), err
	}
	if !outmix.Valid(c.OutputMode) {
		c.OutputMode = outmix.Stereo
	}
	if c.ChannelOffset < 0 || c.ChannelOffset > 63 {
		c.ChannelOffset = 0
	}
	if c.MidiChannel < -1 || c.MidiChannel > 15 {
		c.MidiChannel = -1
	}
	if c.PCMDevice == "" {
		c.PCMDevice = DefaultConfig().PCMDevice
	}
	return c, nil
}

// SaveConfig writes the config atomically.
func SaveConfig(dir string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, ConfigFile), b)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
