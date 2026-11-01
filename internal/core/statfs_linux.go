//go:build linux

package core

import "syscall"

type syscallStatfs struct{ s syscall.Statfs_t }

func statfs(p string, st *syscallStatfs) error { return syscall.Statfs(p, &st.s) }
func (s *syscallStatfs) free() uint64          { return s.s.Bavail * uint64(s.s.Bsize) }
