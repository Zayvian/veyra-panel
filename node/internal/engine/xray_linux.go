package engine

import "syscall"

// xraySysProcAttr has the kernel stop Xray if this process dies without
// stopping it. Otherwise a crashed node leaves an Xray holding its ports, and
// the node that systemd starts next cannot bind them.
func xraySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
