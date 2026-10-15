package background

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreviewMetadataIsReusedUntilFilesChange(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "preview")
	if err := os.WriteFile(prefix+".type", []byte("image/png"), 0644); err != nil {
		t.Fatal(err)
	}
	write := func(fill color.NRGBA, modified time.Time) {
		t.Helper()
		image := solidPNGBuffer(fill)
		if err := os.WriteFile(prefix+".bin", image.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(prefix+".bin", modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	modified := time.Unix(1700000000, 0)
	write(color.NRGBA{R: 220, A: 255}, modified)
	loads := 0
	loader := func() ([]byte, string, error) {
		loads++
		data, err := os.ReadFile(prefix + ".bin")
		return data, "image/png", err
	}
	firstURL, firstColor := resolvePreviewMetadata(prefix, loader)
	secondURL, secondColor := resolvePreviewMetadata(prefix, loader)
	if firstURL == "" || firstColor == "" || secondURL != firstURL || secondColor != firstColor || loads != 1 {
		t.Fatalf("unchanged preview must be processed once: loads=%d colors=%q/%q", loads, firstColor, secondColor)
	}
	write(color.NRGBA{B: 220, A: 255}, modified.Add(time.Second))
	nextURL, nextColor := resolvePreviewMetadata(prefix, loader)
	if nextURL == firstURL || nextColor == firstColor || loads != 2 {
		t.Fatalf("changed preview must invalidate metadata: loads=%d color=%q", loads, nextColor)
	}
	if err := os.Remove(prefix + ".bin"); err != nil {
		t.Fatal(err)
	}
	missingURL, missingColor := resolvePreviewMetadata(prefix, loader)
	if missingURL != "" || missingColor != "" || loads != 3 {
		t.Fatal("deleted preview must not keep stale metadata")
	}
}

func TestCacheExistsRequiresRegularNonemptyVariantFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	source := "https://example.com/background.png"
	if err := writeCachedVariant(source, "full", []byte("image"), "image/png"); err != nil {
		t.Fatal(err)
	}
	if !cacheExists(source, "full") {
		t.Fatal("expected complete cached variant")
	}
	prefix := filepath.Join(cacheDir, remoteCacheKey(source)+"-full")
	if err := os.Remove(prefix + ".type"); err != nil {
		t.Fatal(err)
	}
	if cacheExists(source, "full") {
		t.Fatal("variant with missing content type must not be considered ready")
	}
	if err := os.Mkdir(prefix+".type", 0755); err != nil {
		t.Fatal(err)
	}
	if cacheExists(source, "full") {
		t.Fatal("a directory must not be considered a cached variant file")
	}
}
