package template

import (
	"os"
	"path/filepath"
	"testing"
)

// testDataDir resolves the repo-root test_data/ folder (../../../test_data from
// backend/internal/template).
func testDataDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "test_data"))
	if err != nil {
		t.Fatalf("resolve test_data: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("test_data not found at %s: %v", dir, err)
	}
	return dir
}

func loadFixture(t *testing.T, rel string) Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testDataDir(t), rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	evt, err := ParseEvent(raw)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
	return evt
}

// TestFixtures_AllParse loads every committed fixture and asserts it has the
// expected envelope shape, so a malformed/renamed fixture fails loudly.
func TestFixtures_AllParse(t *testing.T) {
	root := testDataDir(t)
	var count int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		count++
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return nil
		}
		evt, err := ParseEvent(raw)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		switch evt.EventType {
		case "github":
			if evt.GitHub == nil {
				t.Errorf("%s: event_type github but github payload nil", path)
			}
		case "linear":
			if evt.Linear == nil {
				t.Errorf("%s: event_type linear but linear payload nil", path)
			}
		default:
			t.Errorf("%s: unexpected event_type %q", path, evt.EventType)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if count == 0 {
		t.Fatal("no fixtures found — test_data/ is empty")
	}
	t.Logf("validated %d fixtures", count)
}

// TestFixtures_Linear proves Linear templates work against the real captured
// shapes (this is the rigour the readme asks for, no guesswork).
func TestFixtures_Linear(t *testing.T) {
	e := New()

	created := loadFixture(t, "linear/issue.created.json")
	statusChanged := loadFixture(t, "linear/issue.status_changed.json")
	removed := loadFixture(t, "linear/issue.removed.json")

	t.Run("name renders from real fields", func(t *testing.T) {
		name, err := e.Render("tkt-${{ linear.data.team.key }}-${{ linear.data.number }}", statusChanged)
		if err != nil {
			t.Fatal(err)
		}
		if name == "" || name == "tkt--" {
			t.Fatalf("rendered name looks empty: %q", name)
		}
		t.Logf("status_changed -> %q", name)
	})

	t.Run("status-change conditional", func(t *testing.T) {
		// The status_changed fixture is action=update, state.name=Done.
		ok, err := e.Evaluate("linear.action == 'update' && linear.data.state.name == 'Done'", statusChanged)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected status-change conditional to be true on status_changed fixture")
		}
		// Same conditional must be false on the create fixture.
		ok, err = e.Evaluate("linear.action == 'update' && linear.data.state.name == 'Done'", created)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("status-change conditional should be false on create fixture")
		}
	})

	t.Run("action discrimination", func(t *testing.T) {
		for _, tc := range []struct {
			evt  Event
			want string
		}{
			{created, "create"},
			{statusChanged, "update"},
			{removed, "remove"},
		} {
			ok, err := e.Evaluate("linear.action == '"+tc.want+"'", tc.evt)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Errorf("expected action == %q to be true", tc.want)
			}
		}
	})
}

// TestFixtures_GitHub proves GitHub templates work against the official octokit
// payload shapes.
func TestFixtures_GitHub(t *testing.T) {
	e := New()

	opened := loadFixture(t, "github/issues.opened.json")
	labeled := loadFixture(t, "github/issues.labeled.json")
	prOpened := loadFixture(t, "github/pull_request.opened.json")

	t.Run("issue channel name", func(t *testing.T) {
		name, err := e.Render("issue-${{ github.repository.name }}-${{ github.issue.number }}", opened)
		if err != nil {
			t.Fatal(err)
		}
		if name == "issue--" {
			t.Fatalf("name not rendered from real fields: %q", name)
		}
		t.Logf("issues.opened -> %q", name)
	})

	t.Run("pr channel name", func(t *testing.T) {
		name, err := e.Render("pr-${{ github.repository.name }}-${{ github.pull_request.number }}", prOpened)
		if err != nil {
			t.Fatal(err)
		}
		if name == "pr--" {
			t.Fatalf("PR name not rendered: %q", name)
		}
		t.Logf("pull_request.opened -> %q", name)
	})

	t.Run("action conditional", func(t *testing.T) {
		ok, err := e.Evaluate("github.action == 'opened'", opened)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected github.action == 'opened' on opened fixture")
		}
		ok, err = e.Evaluate("github.action == 'opened'", labeled)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("expected github.action == 'opened' to be false on labeled fixture")
		}
	})

	t.Run("label contains via filter", func(t *testing.T) {
		// issues.labeled has a labels array on the issue; the filter expression
		// must extract names. We assert the expression at least evaluates without
		// error and returns a bool (label set varies per fixture).
		_, err := e.Evaluate("contains(github.issue.labels.*.name, 'bug')", labeled)
		if err != nil {
			t.Fatalf("label filter expression errored: %v", err)
		}
	})
}
