package background

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestBackgroundRejectsOversizedDecodedImageBeforeDecoding(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	binary.BigEndian.PutUint32(raw[16:20], 65535)
	binary.BigEndian.PutUint32(raw[20:24], 65535)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	if _, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil {
		t.Fatalf("invalid test header: %v", err)
	}
	if _, _, _, _, err := makeVariants(raw, "image/png"); err == nil {
		t.Fatal("oversized decoded image accepted")
	}
	if _, _, err := makePreviewOnly(raw, "image/png"); err == nil {
		t.Fatal("oversized preview accepted")
	}
	if got := representativeColor(raw, "image/png"); got != "" {
		t.Fatal("oversized image was decoded for its accent color")
	}
}

func TestBackgroundDecodeConcurrencyIsBounded(t *testing.T) {
	for range backgroundProcessLimit {
		if err := acquireBackgroundDecodeSlot(); err != nil {
			t.Fatal(err)
		}
		defer func() { <-decodeSlots }()
	}
	_, _, _, _, err := makeVariants([]byte("<svg/>"), "image/svg+xml")
	if !errors.Is(err, ErrBackgroundBusy) {
		t.Fatalf("expected busy error, got %v", err)
	}
}

func TestBackgroundFetchConcurrencyAndCancellation(t *testing.T) {
	t.Chdir(t.TempDir())
	for range backgroundProcessLimit {
		fetchSlots <- struct{}{}
		defer func() { <-fetchSlots }()
	}
	_, _, err := FetchRemoteVariant("https://unused.invalid/background.png", "full")
	if !errors.Is(err, ErrBackgroundBusy) {
		t.Fatalf("expected fetch limit before networking, got %v", err)
	}
	const source = "https://pending.invalid/background.png"
	key := remoteCacheKey(source)
	inflight.Store(key, make(chan struct{}))
	defer inflight.Delete(key)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = FetchRemoteVariantContext(ctx, source, "full")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pending fetch ignored cancellation: %v", err)
	}
}
