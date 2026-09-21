package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetEnvScalars(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		check func(t *testing.T)
	}{
		{"string set", " hello ", true, func(t *testing.T) {
			if got := GetEnv("K", "def"); got != "hello" {
				t.Fatalf("got %q", got)
			}
		}},
		{"string empty falls back", "   ", true, func(t *testing.T) {
			if got := GetEnv("K", "def"); got != "def" {
				t.Fatalf("got %q", got)
			}
		}},
		{"string unset falls back", "", false, func(t *testing.T) {
			if got := GetEnv("K", "def"); got != "def" {
				t.Fatalf("got %q", got)
			}
		}},
		{"int ok", "42", true, func(t *testing.T) {
			if got := GetEnvInt("K", 7); got != 42 {
				t.Fatalf("got %d", got)
			}
		}},
		{"int garbage falls back", "notanint", true, func(t *testing.T) {
			if got := GetEnvInt("K", 7); got != 7 {
				t.Fatalf("got %d", got)
			}
		}},
		{"float ok", " 1.5 ", true, func(t *testing.T) {
			if got := GetEnvFloat("K", 0); got != 1.5 {
				t.Fatalf("got %v", got)
			}
		}},
		{"float garbage falls back", "1.5.5", true, func(t *testing.T) {
			if got := GetEnvFloat("K", 0.25); got != 0.25 {
				t.Fatalf("got %v", got)
			}
		}},
		{"duration ok", "90s", true, func(t *testing.T) {
			if got := GetEnvDuration("K", time.Second); got != 90*time.Second {
				t.Fatalf("got %v", got)
			}
		}},
		{"duration garbage falls back", "90", true, func(t *testing.T) {
			if got := GetEnvDuration("K", time.Second); got != time.Second {
				t.Fatalf("got %v", got)
			}
		}},
		{"bool ok", "true", true, func(t *testing.T) {
			if !GetEnvBool("K", false) {
				t.Fatal("want true")
			}
		}},
		{"bool garbage falls back", "yes please", true, func(t *testing.T) {
			if !GetEnvBool("K", true) {
				t.Fatal("want default true")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("K")
			if tt.set {
				t.Setenv("K", tt.value)
			}
			tt.check(t)
		})
	}
}

func TestGetEnvList(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		set  bool
		want []string
	}{
		{"unset", "", false, []string{"a"}},
		{"trims and splits", " x , y ,, z ", true, []string{"x", "y", "z"}},
		{"only separators falls back", " , , ", true, []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("L")
			if tt.set {
				t.Setenv("L", tt.raw)
			}
			got := GetEnvList("L", []string{"a"})
			if len(got) != len(tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v want %v", got, tt.want)
				}
			}
		})
	}
}

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# comment\n\nNEW_KEY=fresh\nQUOTED=\"quoted value\"\nEXISTING=from_file\nbroken line\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXISTING", "from_env")
	os.Unsetenv("NEW_KEY")
	os.Unsetenv("QUOTED")
	t.Cleanup(func() { os.Unsetenv("NEW_KEY"); os.Unsetenv("QUOTED") })

	LoadDotEnv(path)

	if got := os.Getenv("NEW_KEY"); got != "fresh" {
		t.Fatalf("NEW_KEY = %q", got)
	}
	if got := os.Getenv("QUOTED"); got != "quoted value" {
		t.Fatalf("QUOTED = %q", got)
	}
	if got := os.Getenv("EXISTING"); got != "from_env" {
		t.Fatalf("real env must win, got %q", got)
	}

	LoadDotEnv(filepath.Join(t.TempDir(), "missing.env")) // no-op, must not panic
}
