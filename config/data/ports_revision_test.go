package data

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/junfuchang/superflare/config/model"
)

func TestConcurrentPortSnapshotsAllowOnlyOneWriter(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := SavePortBindings(model.Ports{}); err != nil {
		t.Fatal(err)
	}
	_, original, err := GetPortBindingMapWithRevision()
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsByWriter := make([]error, 2)
	revisions := make([]string, 2)
	var wg sync.WaitGroup
	for i := range errorsByWriter {
		wg.Go(func() {
			<-start
			revisions[i], errorsByWriter[i] = UpdatePortRemarksWithRevision([]model.PortBinding{{Port: 8080 + i, Remark: "winner"}}, original, true)
		})
	}
	close(start)
	wg.Wait()
	bindings, current, err := GetPortBindingMapWithRevision()
	if err != nil || len(bindings) != 1 {
		t.Fatalf("invalid final state: %#v, %v", bindings, err)
	}
	winners, conflicts := 0, 0
	for i, err := range errorsByWriter {
		switch {
		case err == nil:
			winners++
			if revisions[i] != current || bindings[portBindingKey("tcp", 8080+i)].Remark != "winner" {
				t.Fatalf("saved snapshot does not match winning writer %d", i)
			}
		case errors.Is(err, ErrPortRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected save error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("got %d winners and %d conflicts", winners, conflicts)
	}
}

func TestPortRevisionDetectsExternalEditsWithUnchangedFileMetadata(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := SavePortBindings(model.Ports{Items: []model.PortBinding{{Port: 8080, Remark: "before"}}}); err != nil {
		t.Fatal(err)
	}
	_, revision, err := GetPortBindingMapWithRevision()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPortBindings(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat("ports.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("ports.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("before"), []byte("edited"))
	if err := os.WriteFile("ports.yaml", raw, info.Mode()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes("ports.yaml", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdatePortRemarksWithRevision(nil, revision, true); !errors.Is(err, ErrPortRevisionConflict) {
		t.Fatalf("external edit was overwritten: %v", err)
	}
	bindings, _, err := GetPortBindingMapWithRevision()
	if err != nil || bindings["tcp:8080"].Remark != "edited" {
		t.Fatalf("external edit missing: %#v, %v", bindings, err)
	}
}
