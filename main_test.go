package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/rustkill/internal/contract"
	"github.com/Matthew-Eucaristo/rustkill/internal/testfix"
)

// capture runs rustkill with args and returns stdout and the exit code.
func capture(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	r, w, _ := os.Pipe()
	oldOut, oldIn := os.Stdout, os.Stdin
	os.Stdout = w
	if stdin != "" {
		ir, iw, _ := os.Pipe()
		go func() { iw.WriteString(stdin); iw.Close() }()
		os.Stdin = ir
	}
	done := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); done <- b }()
	err := run(args)
	w.Close()
	os.Stdout, os.Stdin = oldOut, oldIn
	out := <-done
	code := 0
	if ee, ok := err.(*exitError); ok {
		code = ee.code
	} else if err != nil {
		t.Fatalf("rustkill %v: %v", args, err)
	}
	return string(out), code
}

// setup makes a fake home with two idle crates under ~/dev.
func setup(t *testing.T) (root, app string) {
	home := testfix.Real(t, t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CARGO_HOME", filepath.Join(home, ".cargo"))
	t.Setenv("RUSTUP_HOME", filepath.Join(home, ".rustup"))
	root = filepath.Join(home, "dev")
	app = filepath.Join(root, "shop")
	testfix.Write(t, app, testfix.Target(testfix.Crate("shop"), "target"))
	testfix.Repo(t, app, testfix.Old)
	cli := filepath.Join(root, "cli")
	testfix.Write(t, cli, testfix.Target(testfix.Crate("cli"), "target"))
	testfix.Repo(t, cli, testfix.Old)
	return root, app
}

func TestScanJSONIsHomeRelative(t *testing.T) {
	root, _ := setup(t)
	out, code := capture(t, "", "scan", "--json", "--no-global", root)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var doc contract.ScanDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != 1 || doc.Tool.Name != "rustkill" || doc.Tool.Ecosystem != "rust" || len(doc.Items) != 2 || doc.Summary.Projects != 2 {
		t.Fatalf("doc %+v", doc.Summary)
	}
	home, _ := os.UserHomeDir()
	if strings.Contains(out, home) {
		t.Error("JSON contains the home path")
	}
	for _, it := range doc.Items {
		if !strings.HasPrefix(it.Path, filepath.Join("~", "dev")+string(filepath.Separator)) || it.Safety.Level != "safe" || it.Action != "trash" || it.Project == nil || it.Git == nil || !it.Git.Ignored {
			t.Errorf("item %+v", it)
		}
	}
}

func TestNDJSONStreams(t *testing.T) {
	root, _ := setup(t)
	out, _ := capture(t, "", "scan", "--ndjson", "--no-global", root)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], `{"type":"header"`) || !strings.HasPrefix(lines[3], `{"type":"summary"`) {
		t.Fatalf("lines %v", lines)
	}
}

func TestApplyPlanDryRunAndStale(t *testing.T) {
	root, app := setup(t)
	out, _ := capture(t, "", "scan", "--json", "--no-global", root)
	var doc contract.ScanDoc
	json.Unmarshal([]byte(out), &doc)
	var nm contract.Item
	for _, it := range doc.Items {
		if it.Project.Name == "shop" {
			nm = it
		}
	}
	plan := `{"schema_version":1,"items":[{"id":"` + nm.ID + `","fingerprint":"` + nm.Fingerprint + `"}]}`
	res, code := capture(t, plan, "apply", "--plan", "-", "--dry-run", "--json", root)
	if code != 0 || !strings.Contains(res, `"status": "dry_run"`) {
		t.Fatalf("dry run: %d %s", code, res)
	}
	if _, err := os.Stat(filepath.Join(app, "target")); err != nil {
		t.Fatal("dry run removed it")
	}
	stale := strings.Replace(plan, nm.Fingerprint, "fp-0000000000000000", 1)
	if _, code := capture(t, stale, "apply", "--plan", "-", "--json", root); code != contract.ExitRefused {
		t.Fatalf("stale plan exit %d", code)
	}
	if _, code := capture(t, `{"schema_version":1,"items":[{"id":"x","fingerprint":"y","force":true}]}`, "apply", "--plan", "-", root); code != contract.ExitUsage {
		t.Fatalf("unknown plan field exit %d", code)
	}
}

func TestHeadlessCleansOnlySafeAndIdle(t *testing.T) {
	root, app := setup(t)
	// Idle for at most a year: the 120-day-old project is too recent.
	if out, code := capture(t, "", "--yes", "--older-than", "1y", "--permanent", root); code != 0 || !strings.Contains(out, "nothing safe") {
		t.Fatalf("%d %s", code, out)
	}
	out, code := capture(t, "", "--yes", "--older-than", "30d", "--permanent", "--json", root)
	if code != 0 || !strings.Contains(out, `"done": 2`) {
		t.Fatalf("%d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(app, "target")); !os.IsNotExist(err) {
		t.Error("target still there")
	}
	if _, err := os.Stat(filepath.Join(app, "src", "main.rs")); err != nil {
		t.Fatal("source removed")
	}
}

func TestCapabilitiesAndUsage(t *testing.T) {
	out, code := capture(t, "", "capabilities", "--json")
	var c contract.Capabilities
	if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.SchemaVersion != 1 || len(c.Kinds) == 0 || len(c.Reasons) == 0 {
		t.Fatalf("capabilities: %d %s", code, out)
	}
	if _, code := capture(t, "", "--dry-run", "--permanent", "--list", "."); code != contract.ExitUsage {
		t.Errorf("conflicting flags exit %d", code)
	}
	if _, code := capture(t, "", "--older-than", "soon", "--list", "."); code != contract.ExitUsage {
		t.Errorf("bad age exit %d", code)
	}
}
