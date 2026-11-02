//go:build !linux

package engine

import "syscall"

func xraySysProcAttr() *syscall.SysProcAttr { return nil }
