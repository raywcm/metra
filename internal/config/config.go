package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Settings struct {
	IntervalMS   int    `json:"interval_ms"`
	RangeSeconds int    `json:"range_seconds"`
	Theme        string `json:"theme"`
	Grid         bool   `json:"grid"`
	Language     string `json:"language"`
}

func Defaults() Settings {
	return Settings{IntervalMS: 1000, RangeSeconds: 60, Theme: "auto", Grid: false, Language: "auto"}
}
func (s Settings) Interval() time.Duration { return time.Duration(s.IntervalMS) * time.Millisecond }
func (s Settings) Validate() error {
	if s.IntervalMS < 500 || s.IntervalMS > 10000 {
		return errors.New("interval must be 500–10000 ms")
	}
	if s.RangeSeconds < 10 || s.RangeSeconds > 900 {
		return errors.New("chart range must be 10–900 seconds")
	}
	switch s.Theme {
	case "auto", "dark", "midnight", "light", "forest":
	default:
		return fmt.Errorf("unknown theme %q", s.Theme)
	}
	switch s.Language {
	case "auto", "en", "zh-CN":
	default:
		return fmt.Errorf("unknown language %q", s.Language)
	}
	return nil
}
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "metra", "settings.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "metra", "settings.json"), nil
}
func Load(path string) (Settings, error) {
	s := Defaults()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return Defaults(), err
	}
	if err = s.Validate(); err != nil {
		return Defaults(), err
	}
	return s, nil
}
func Save(path string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
