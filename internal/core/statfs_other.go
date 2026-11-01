//go:build !linux

package core

type syscallStatfs struct{}

func statfs(p string, st *syscallStatfs) error { return nil }
func (s *syscallStatfs) free() uint64          { return 0 }
