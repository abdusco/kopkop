package filesystem

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MemoryFS supports io/fs reads and confined mutable file operations.
// Permissions are metadata, not an access-control mechanism. Open handles
// snapshot bytes, entries and metadata; filesystem mutations remain independent.
type MemoryFS struct {
	mu    sync.RWMutex
	files map[string]memoryEntry
	dirs  map[string]memoryFileInfo
}

type memoryEntry struct {
	data []byte
	info memoryFileInfo
}

func NewMemoryFS() *MemoryFS {
	return &MemoryFS{
		files: map[string]memoryEntry{},
		dirs:  map[string]memoryFileInfo{".": {name: ".", mode: fs.ModeDir | 0o755, modTime: time.Now()}},
	}
}

func (m *MemoryFS) Open(name string) (fs.File, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.checkParentsLocked(name); err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	if entry, ok := m.files[name]; ok {
		return &memoryFile{name: name, info: entry.info, r: bytes.NewReader(bytes.Clone(entry.data))}, nil
	}
	if info, ok := m.dirs[name]; ok {
		return &memoryDir{name: name, info: info, entries: m.readDirLocked(name)}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (m *MemoryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.checkParentsLocked(name); err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	if _, ok := m.files[name]; ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: syscall.ENOTDIR}
	}
	if _, ok := m.dirs[name]; !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return m.readDirLocked(name), nil
}

func (m *MemoryFS) readDirLocked(name string) []fs.DirEntry {
	entries := make([]fs.DirEntry, 0)
	for dir, info := range m.dirs {
		if dir != "." && path.Dir(dir) == name {
			entries = append(entries, memoryDirEntry{info: info})
		}
	}
	for file, entry := range m.files {
		if path.Dir(file) == name {
			entries = append(entries, memoryDirEntry{info: entry.info})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries
}

func (m *MemoryFS) ReadFile(name string) ([]byte, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.checkParentsLocked(name); err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	if _, ok := m.dirs[name]; ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: syscall.EISDIR}
	}
	entry, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(entry.data), nil
}

func (m *MemoryFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.dirs[name]; ok {
		return &fs.PathError{Op: "write", Path: name, Err: syscall.EISDIR}
	}
	if err := m.checkParentsLocked(name); err != nil {
		return &fs.PathError{Op: "write", Path: name, Err: err}
	}
	now := time.Now()
	m.mkdirAllLocked(path.Dir(name), 0o755, now)
	mode := perm.Perm()
	if old, ok := m.files[name]; ok {
		mode = old.info.mode
	} else {
		m.touchDirLocked(path.Dir(name), now)
	}
	m.files[name] = memoryEntry{data: bytes.Clone(data), info: memoryFileInfo{
		name: path.Base(name), size: int64(len(data)), mode: mode, modTime: now,
	}}
	return nil
}

func (m *MemoryFS) MkdirAll(name string, perm fs.FileMode) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; ok {
		return &fs.PathError{Op: "mkdir", Path: name, Err: syscall.ENOTDIR}
	}
	if err := m.checkParentsLocked(name); err != nil {
		return &fs.PathError{Op: "mkdir", Path: name, Err: err}
	}
	m.mkdirAllLocked(name, perm.Perm(), time.Now())
	return nil
}

func (m *MemoryFS) Stat(name string) (fs.FileInfo, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.checkParentsLocked(name); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	if entry, ok := m.files[name]; ok {
		return entry.info, nil
	}
	if info, ok := m.dirs[name]; ok {
		return info, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func (m *MemoryFS) RemoveAll(name string) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkParentsLocked(name); err != nil {
		return &fs.PathError{Op: "remove", Path: name, Err: err}
	}
	now := time.Now()
	if name == "." {
		clear(m.files)
		root := m.dirs["."]
		root.modTime = now
		m.dirs = map[string]memoryFileInfo{".": root}
		return nil
	}
	_, fileExists := m.files[name]
	_, dirExists := m.dirs[name]
	for file := range m.files {
		if file == name || strings.HasPrefix(file, name+"/") {
			delete(m.files, file)
		}
	}
	for dir := range m.dirs {
		if dir == name || strings.HasPrefix(dir, name+"/") {
			delete(m.dirs, dir)
		}
	}
	if fileExists || dirExists {
		m.touchDirLocked(path.Dir(name), now)
	}
	return nil
}

// Check every ancestor before mutation to reject file/directory conflicts
// without creating partial directory trees.
func (m *MemoryFS) checkParentsLocked(name string) error {
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if _, ok := m.files[parent]; ok {
			return syscall.ENOTDIR
		}
	}
	return nil
}

func (m *MemoryFS) mkdirAllLocked(name string, perm fs.FileMode, now time.Time) {
	if name == "." {
		return
	}
	if _, exists := m.dirs[name]; exists {
		return
	}
	m.mkdirAllLocked(path.Dir(name), perm, now)
	m.dirs[name] = memoryFileInfo{name: path.Base(name), mode: fs.ModeDir | perm, modTime: now}
	m.touchDirLocked(path.Dir(name), now)
}

func (m *MemoryFS) touchDirLocked(name string, now time.Time) {
	info := m.dirs[name]
	info.modTime = now
	m.dirs[name] = info
}

type memoryFile struct {
	mu     sync.Mutex
	name   string
	info   memoryFileInfo
	r      *bytes.Reader
	closed bool
}

func (f *memoryFile) Stat() (fs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, &fs.PathError{Op: "stat", Path: f.name, Err: fs.ErrClosed}
	}
	return f.info, nil
}

func (f *memoryFile) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrClosed}
	}
	return f.r.Read(p)
}

func (f *memoryFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return &fs.PathError{Op: "close", Path: f.name, Err: fs.ErrClosed}
	}
	f.closed = true
	return nil
}

type memoryDir struct {
	mu      sync.Mutex
	name    string
	info    memoryFileInfo
	entries []fs.DirEntry
	offset  int
	closed  bool
}

func (d *memoryDir) Stat() (fs.FileInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, &fs.PathError{Op: "stat", Path: d.name, Err: fs.ErrClosed}
	}
	return d.info, nil
}

func (d *memoryDir) Read(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrClosed}
	}
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: syscall.EISDIR}
}

func (d *memoryDir) ReadDir(n int) ([]fs.DirEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, &fs.PathError{Op: "readdir", Path: d.name, Err: fs.ErrClosed}
	}
	remaining := len(d.entries) - d.offset
	if n > 0 && remaining == 0 {
		return nil, io.EOF
	}
	count := remaining
	if n > 0 && n < count {
		count = n
	}
	entries := make([]fs.DirEntry, count)
	copy(entries, d.entries[d.offset:d.offset+count])
	d.offset += count
	return entries, nil
}

func (d *memoryDir) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return &fs.PathError{Op: "close", Path: d.name, Err: fs.ErrClosed}
	}
	d.closed = true
	return nil
}

type memoryFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (fi memoryFileInfo) Name() string       { return fi.name }
func (fi memoryFileInfo) Size() int64        { return fi.size }
func (fi memoryFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi memoryFileInfo) ModTime() time.Time { return fi.modTime }
func (fi memoryFileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi memoryFileInfo) Sys() any           { return nil }

type memoryDirEntry struct{ info memoryFileInfo }

func (e memoryDirEntry) Name() string               { return e.info.Name() }
func (e memoryDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e memoryDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e memoryDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

var _ fs.ReadDirFS = (*MemoryFS)(nil)
var _ fs.ReadFileFS = (*MemoryFS)(nil)
var _ fs.StatFS = (*MemoryFS)(nil)
var _ fs.ReadDirFile = (*memoryDir)(nil)
