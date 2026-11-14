package fn

import (
	"testing"
	"time"
)

// The render path only needs to know whether a downloaded icon exists; its
// cost should not grow with the number of bytes in the cached image.
func BenchmarkSiteFaviconMemoryLookup(b *testing.B) {
	const source = "https://memory-benchmark.example.com/favicon.ico"
	siteIconMemory.Store(source, make([]byte, 64*1024), "image/png", time.Now())
	b.Cleanup(func() { siteIconMemory.Delete(source) })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !hasValidatedSiteFaviconFast(source) {
			b.Fatal("cached icon was not found")
		}
	}
}
