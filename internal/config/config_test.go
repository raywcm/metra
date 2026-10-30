package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metra", "settings.json")
	s, e := Load(path)
	if e != nil || s != Defaults() {
		t.Fatal(s, e)
	}
	s.Theme = "forest"
	s.Language = "zh-CN"
	s.IntervalMS = 500
	if e = Save(path, s); e != nil {
		t.Fatal(e)
	}
	got, e := Load(path)
	if e != nil || got != s {
		t.Fatal(got, e)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("settings permissions")
	}
	os.WriteFile(path, []byte(`{"interval_ms":0}`), 0600)
	got, e = Load(path)
	if e == nil || got != Defaults() {
		t.Fatal("invalid config accepted")
	}
	s.RangeSeconds = 901
	if Save(path, s) == nil {
		t.Fatal("invalid save accepted")
	}
}
func TestPartialAndMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"theme":"light"}`), 0600)
	s, e := Load(path)
	if e != nil || s.IntervalMS != 1000 || s.Theme != "light" {
		t.Fatal(s, e)
	}
	os.WriteFile(path, []byte(`{`), 0600)
	if _, e = Load(path); e == nil {
		t.Fatal("accepted malformed JSON")
	}
}

func TestLegacyConfigGetsAutomaticLanguage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"interval_ms":1000,"range_seconds":60,"theme":"midnight","grid":true}`), 0600)
	s, err := Load(p)
	if err != nil || s.Language != "auto" || s.Theme != "midnight" {
		t.Fatal(s, err)
	}
	if Defaults().Theme != "auto" || Defaults().Language != "auto" {
		t.Fatal("new defaults should adapt automatically")
	}
	if Defaults().Grid {
		t.Fatal("grid should be disabled by default")
	}
	s.Language = "invalid"
	if s.Validate() == nil {
		t.Fatal("invalid language accepted")
	}

}

func TestRemovedChartStyleMigratesWithoutResettingSettings(t *testing.T) {
	for _, legacy := range []string{"braille", "blocks"} {
		path := filepath.Join(t.TempDir(), "settings.json")
		data := []byte(`{"chart_style":"` + legacy + `","theme":"forest","language":"zh-CN","interval_ms":2000,"range_seconds":120}`)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got.Theme != "forest" || got.Language != "zh-CN" || got.IntervalMS != 2000 || got.RangeSeconds != 120 {
			t.Fatal(got, err)
		}
		if err = Save(path, got); err != nil {
			t.Fatal(err)
		}
		saved, _ := os.ReadFile(path)
		if strings.Contains(string(saved), "chart_style") {
			t.Fatal("obsolete style saved")
		}
	}
}
