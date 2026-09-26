package llm

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

// ggml-rpc-server's -c tensor cache (tools/rpc/rpc-server.cpp and
// ggml/src/ggml-rpc/ggml-rpc.cpp, checked at b11081) stores each weight
// tensor over 10 MiB as <dir>/<16 hex digits>, named by the 64-bit FNV-1a
// hash of its bytes, and never deletes anything. It doesn't check its own
// writes either: a full disk or a worker killed mid-write leaves a short
// file under the right name, which it later serves as a cache hit without
// re-hashing it, so that tensor is silently corrupt on every reload.
// tidyRPCCache works around both from outside until llama.cpp is fixed.
const (
	// rpcCacheMinFree is the disk space tidyRPCCache keeps free, deleting
	// cache files if it has to, and below which the worker runs uncached.
	rpcCacheMinFree = 10 << 30
	// rpcCacheSettle: files written more recently than this may still be
	// mid-write, so their hash is checked on a later pass.
	rpcCacheSettle = time.Minute
	// rpcCacheTidyInterval is how often a running worker's cache is tidied.
	rpcCacheTidyInterval = 5 * time.Minute
	// rpcCacheCheckedMarker's modification time records how far hash checks
	// have got, so a restart only re-checks files written since.
	rpcCacheCheckedMarker = "checked-until"
)

var rpcCacheFileName = regexp.MustCompile(`^[0-9a-f]{16}$`)

// tidyRPCCache deletes cache files in dir whose contents don't match their
// name, then the oldest files until the cache is at most maxBytes and the
// disk has rpcCacheMinFree left. It reports whether that much is free
// afterwards, i.e. whether there's room to keep caching.
//
// ponytail: "oldest" is by write time -- llama.cpp never touches a file it
// serves, so there's no last-used time to go by. A true LRU needs the
// llama.cpp side to record hits.
func tidyRPCCache(dir string, maxBytes uint64, now time.Time, diskFree func(string) (uint64, error)) bool {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("cluster: can't read RPC cache", "dir", dir, "error", err)
	}

	marker := filepath.Join(dir, rpcCacheCheckedMarker)
	var checkedSince time.Time
	if info, err := os.Stat(marker); err == nil {
		checkedSince = info.ModTime()
	}
	checkedUntil := now.Add(-rpcCacheSettle)

	type cacheFile struct {
		path    string
		size    uint64
		written time.Time
	}
	var files []cacheFile
	var total uint64
	for _, e := range entries {
		if !e.Type().IsRegular() || !rpcCacheFileName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		written := info.ModTime()
		if written.After(checkedSince) && !written.After(checkedUntil) && !rpcCacheFileIntact(path, e.Name()) {
			slog.Warn("cluster: removing corrupt RPC cache file", "file", path)
			os.Remove(path)
			continue
		}
		files = append(files, cacheFile{path, uint64(info.Size()), written})
		total += uint64(info.Size())
	}
	if err == nil {
		if f, err := os.Create(marker); err == nil {
			f.Close()
			os.Chtimes(marker, checkedUntil, checkedUntil)
		}
	}

	free := func() uint64 {
		n, err := diskFree(dir)
		if err != nil {
			return math.MaxUint64 // unknown: don't delete for space
		}
		return n
	}
	slices.SortFunc(files, func(a, b cacheFile) int { return a.written.Compare(b.written) })
	for len(files) > 0 && (total > maxBytes || free() < rpcCacheMinFree) {
		os.Remove(files[0].path)
		total -= files[0].size
		files = files[1:]
	}
	return free() >= rpcCacheMinFree
}

// rpcCacheFileIntact reports whether the file's FNV-1a hash is its name,
// the way ggml-rpc.cpp names it ("%016" PRIx64).
func rpcCacheFileIntact(path, name string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := fnv.New64a()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return fmt.Sprintf("%016x", h.Sum64()) == name
}
