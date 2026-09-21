package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/llm"
)

// How long each model call takes in the benchmarks. Real calls take longer,
// but the time per call only scales the results, and the speedup from
// running calls at once stays the same.
const benchLatency = 20 * time.Millisecond

// A repo of dirs directories with files_per_dir small files each
func makeBenchRepo(b *testing.B, dirs, files_per_dir int) string {
	b.Helper()
	root := filepath.Join(b.TempDir(), "bench")
	for dir := range dirs {
		for file := range files_per_dir {
			file_path := filepath.Join(root, fmt.Sprintf("pkg%d", dir), fmt.Sprintf("file%d.go", file))
			if err := os.MkdirAll(filepath.Dir(file_path), 0755); err != nil {
				b.Fatal(err)
			}
			content := fmt.Sprintf("package pkg%d\n\nfunc F%d() int { return %d }\n", dir, file, dir*files_per_dir+file)
			if err := os.WriteFile(file_path, []byte(content), 0644); err != nil {
				b.Fatal(err)
			}
		}
	}
	return root
}

// Analyze a repo of 64 files in 8 directories with a model that takes 20ms a
// call, making 73 calls at different concurrency levels. "unchanged" analyzes
// it again after a first run, when nothing has to be sent to the model.
//
//	go test -run '^$' -bench Analyze ./internal/analysis
func BenchmarkAnalyze(b *testing.B) {
	root := makeBenchRepo(b, 8, 8)

	for _, concurrency := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("concurrency=%d", concurrency), func(b *testing.B) {
			calls := int64(0)
			for range b.N {
				mock := &llm.MockClient{Respond: respond, Latency: benchLatency}
				// nothing is saved, so every run starts from scratch
				if _, err := Analyze(context.Background(), mock, "bench", root, Options{Concurrency: concurrency}); err != nil {
					b.Fatal(err)
				}
				calls += mock.Calls()
			}
			b.ReportMetric(float64(calls)/float64(b.N), "calls/op")
		})
	}

	b.Run("unchanged", func(b *testing.B) {
		options := Options{SaveDir: b.TempDir(), Concurrency: 16}
		if _, err := Analyze(context.Background(), &llm.MockClient{Respond: respond}, "bench", root, options); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()

		calls := int64(0)
		for range b.N {
			mock := &llm.MockClient{Respond: respond, Latency: benchLatency}
			if _, err := Analyze(context.Background(), mock, "bench", root, options); err != nil {
				b.Fatal(err)
			}
			calls += mock.Calls()
		}
		b.ReportMetric(float64(calls)/float64(b.N), "calls/op")
	})
}
