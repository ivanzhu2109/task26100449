//go:build darwin || linux

package replay

import "syscall"

// peakRSSBytes 返回进程峰值常驻内存（字节）。
func peakRSSBytes() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return ru.Maxrss // darwin 与 linux 上均为字节
}
