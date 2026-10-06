package filesystem

import (
	"fmt"
	"io/fs"
)

// MirrorFS reads from Primary and applies mutations to Primary, then Mirror.
// A failed mutation is returned immediately; successful earlier mutations are
// not rolled back. Callers must discard a failed build rather than publish it.
type MirrorFS struct {
	Primary FileSystem
	Mirror  FileSystem
}

func (m *MirrorFS) Open(name string) (fs.File, error)     { return m.Primary.Open(name) }
func (m *MirrorFS) ReadFile(name string) ([]byte, error)  { return m.Primary.ReadFile(name) }
func (m *MirrorFS) Stat(name string) (fs.FileInfo, error) { return m.Primary.Stat(name) }

func (m *MirrorFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := m.Primary.WriteFile(name, data, perm); err != nil {
		return fmt.Errorf("write primary output: %w", err)
	}
	if err := m.Mirror.WriteFile(name, data, perm); err != nil {
		return fmt.Errorf("write mirrored output: %w", err)
	}
	return nil
}

func (m *MirrorFS) MkdirAll(name string, perm fs.FileMode) error {
	if err := m.Primary.MkdirAll(name, perm); err != nil {
		return fmt.Errorf("mkdir primary output: %w", err)
	}
	if err := m.Mirror.MkdirAll(name, perm); err != nil {
		return fmt.Errorf("mkdir mirrored output: %w", err)
	}
	return nil
}

func (m *MirrorFS) RemoveAll(name string) error {
	if err := m.Primary.RemoveAll(name); err != nil {
		return fmt.Errorf("remove primary output: %w", err)
	}
	if err := m.Mirror.RemoveAll(name); err != nil {
		return fmt.Errorf("remove mirrored output: %w", err)
	}
	return nil
}
