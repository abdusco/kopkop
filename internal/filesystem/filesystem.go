package filesystem

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type FileSystem interface {
	fs.FS
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Stat(name string) (fs.FileInfo, error)
	RemoveAll(path string) error
}

type DiskFS struct {
	Root string
}

func NewDiskFS(root string) *DiskFS {
	return &DiskFS{Root: root}
}

func (f *DiskFS) full(name string) string {
	return filepath.Join(f.Root, filepath.FromSlash(name))
}

func (f *DiskFS) Open(name string) (fs.File, error) {
	return os.Open(f.full(name))
}

func (f *DiskFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(f.full(name))
}

func (f *DiskFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	full := f.full(name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, perm)
}

func (f *DiskFS) MkdirAll(name string, perm fs.FileMode) error {
	return os.MkdirAll(f.full(name), perm)
}

func (f *DiskFS) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(f.full(name))
}

func (f *DiskFS) RemoveAll(name string) error {
	return os.RemoveAll(f.full(name))
}

type MemoryFS struct {
	mu    sync.RWMutex
	files map[string][]byte
	dirs  map[string]struct{}
}

func NewMemoryFS() *MemoryFS {
	return &MemoryFS{
		files: map[string][]byte{},
		dirs:  map[string]struct{}{".": {}},
	}
}

func normalize(name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/"))
	if clean == "." || clean == "" {
		return ".", nil
	}
	if strings.HasPrefix(clean, "../") {
		return "", fs.ErrInvalid
	}
	return clean, nil
}

func (m *MemoryFS) Open(name string) (fs.File, error) {
	n, err := normalize(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if b, ok := m.files[n]; ok {
		dup := make([]byte, len(b))
		copy(dup, b)
		return &memoryFile{name: path.Base(n), data: dup, r: bytes.NewReader(dup)}, nil
	}
	if _, ok := m.dirs[n]; ok {
		return &memoryDir{name: path.Base(n)}, nil
	}
	return nil, fs.ErrNotExist
}

func (m *MemoryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	n, err := normalize(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.dirs[n]; !ok {
		return nil, fs.ErrNotExist
	}
	prefix := ""
	if n != "." {
		prefix = n + "/"
	}
	seen := map[string]fs.DirEntry{}
	for d := range m.dirs {
		if d == "." || d == n || !strings.HasPrefix(d, prefix) {
			continue
		}
		rest := strings.TrimPrefix(d, prefix)
		if strings.Contains(rest, "/") {
			continue
		}
		seen[rest] = memoryDirEntry{name: rest, mode: fs.ModeDir | 0o755}
	}
	for file, b := range m.files {
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		rest := strings.TrimPrefix(file, prefix)
		if strings.Contains(rest, "/") {
			continue
		}
		seen[rest] = memoryDirEntry{name: rest, mode: 0o644, size: int64(len(b))}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]fs.DirEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out, nil
}

func (m *MemoryFS) ReadFile(name string) ([]byte, error) {
	n, err := normalize(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.files[n]
	if !ok {
		return nil, fs.ErrNotExist
	}
	dup := make([]byte, len(b))
	copy(dup, b)
	return dup, nil
}

func (m *MemoryFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	n, err := normalize(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addParentDirsLocked(path.Dir(n))
	dup := make([]byte, len(data))
	copy(dup, data)
	m.files[n] = dup
	return nil
}

func (m *MemoryFS) MkdirAll(name string, perm fs.FileMode) error {
	n, err := normalize(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addParentDirsLocked(n)
	return nil
}

func (m *MemoryFS) Stat(name string) (fs.FileInfo, error) {
	n, err := normalize(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if b, ok := m.files[n]; ok {
		return memoryFileInfo{name: path.Base(n), size: int64(len(b)), mode: 0o644}, nil
	}
	if _, ok := m.dirs[n]; ok {
		return memoryFileInfo{name: path.Base(n), size: 0, mode: fs.ModeDir | 0o755}, nil
	}
	return nil, fs.ErrNotExist
}

func (m *MemoryFS) RemoveAll(name string) error {
	n, err := normalize(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for file := range m.files {
		if file == n || strings.HasPrefix(file, n+"/") {
			delete(m.files, file)
		}
	}
	for d := range m.dirs {
		if d == n || strings.HasPrefix(d, n+"/") {
			delete(m.dirs, d)
		}
	}
	m.dirs["."] = struct{}{}
	return nil
}

func (m *MemoryFS) addParentDirsLocked(name string) {
	cur := name
	for {
		if cur == "." || cur == "" {
			m.dirs["."] = struct{}{}
			return
		}
		m.dirs[cur] = struct{}{}
		next := path.Dir(cur)
		if next == cur {
			return
		}
		cur = next
	}
}

type memoryFile struct {
	name string
	data []byte
	r    *bytes.Reader
}

func (f *memoryFile) Stat() (fs.FileInfo, error) {
	return memoryFileInfo{name: f.name, size: int64(len(f.data)), mode: 0o644}, nil
}
func (f *memoryFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memoryFile) Close() error               { return nil }

type memoryDir struct {
	name string
}

func (d *memoryDir) Stat() (fs.FileInfo, error) {
	return memoryFileInfo{name: d.name, mode: fs.ModeDir | 0o755}, nil
}
func (d *memoryDir) Read(p []byte) (int, error) { return 0, fs.ErrInvalid }
func (d *memoryDir) Close() error               { return nil }

type memoryFileInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (fi memoryFileInfo) Name() string       { return fi.name }
func (fi memoryFileInfo) Size() int64        { return fi.size }
func (fi memoryFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi memoryFileInfo) ModTime() time.Time { return time.Time{} }
func (fi memoryFileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi memoryFileInfo) Sys() any           { return nil }

type memoryDirEntry struct {
	name string
	mode fs.FileMode
	size int64
}

func (e memoryDirEntry) Name() string      { return e.name }
func (e memoryDirEntry) IsDir() bool       { return e.mode.IsDir() }
func (e memoryDirEntry) Type() fs.FileMode { return e.mode.Type() }
func (e memoryDirEntry) Info() (fs.FileInfo, error) {
	return memoryFileInfo{name: e.name, mode: e.mode, size: e.size}, nil
}
