package llm

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCacheFile writes data under the name ggml-rpc-server would give
// full (its FNV-1a hash), so passing a prefix of full as data makes the
// truncated file a failed write leaves behind.
func writeCacheFile(t *testing.T, dir string, full, data []byte, written time.Time) string {
	t.Helper()
	h := fnv.New64a()
	h.Write(full)
	path := filepath.Join(dir, fmt.Sprintf("%016x", h.Sum64()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func plentyFree(string) (uint64, error) { return 1 << 40, nil }

func TestTidyRPCCacheRemovesTruncatedFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-time.Hour)
	tensor := []byte("a weight tensor's bytes")
	good := writeCacheFile(t, dir, tensor, tensor, old)
	truncated := writeCacheFile(t, dir, []byte("another tensor's bytes"), []byte("another"), old)
	fresh := writeCacheFile(t, dir, []byte("still being written"), []byte("still"), now)

	if !tidyRPCCache(dir, 1<<30, now, plentyFree) {
		t.Fatal("tidyRPCCache reported no room with plenty free")
	}
	if !exists(good) || exists(truncated) {
		t.Errorf("good kept = %v (want true), truncated kept = %v (want false)", exists(good), exists(truncated))
	}
	if !exists(fresh) {
		t.Error("a file younger than rpcCacheSettle was checked, but it may still be mid-write")
	}

	// A later pass checks it, and only it: the good file isn't re-read.
	tidyRPCCache(dir, 1<<30, now.Add(2*rpcCacheSettle), plentyFree)
	if exists(fresh) {
		t.Error("truncated file kept after it settled")
	}
}

func TestTidyRPCCacheCapsSizeOldestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tensor := func(s string) []byte { return []byte(fmt.Sprintf("%-100s", s)) }
	oldest := writeCacheFile(t, dir, tensor("a"), tensor("a"), now.Add(-3*time.Hour))
	middle := writeCacheFile(t, dir, tensor("b"), tensor("b"), now.Add(-2*time.Hour))
	newest := writeCacheFile(t, dir, tensor("c"), tensor("c"), now.Add(-1*time.Hour))

	tidyRPCCache(dir, 200, now, plentyFree)
	if exists(oldest) || !exists(middle) || !exists(newest) {
		t.Errorf("kept oldest=%v middle=%v newest=%v, want false true true", exists(oldest), exists(middle), exists(newest))
	}
}

func TestTidyRPCCacheFreesDiskSpace(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tensor := []byte("a weight tensor's bytes")
	path := writeCacheFile(t, dir, tensor, tensor, now.Add(-time.Hour))

	full := func(string) (uint64, error) { return 1 << 20, nil }
	if tidyRPCCache(dir, 1<<30, now, full) {
		t.Error("tidyRPCCache reported room to cache on a full disk")
	}
	if exists(path) {
		t.Error("cache file kept on a full disk")
	}
}
