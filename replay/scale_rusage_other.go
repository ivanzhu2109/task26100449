//go:build !darwin && !linux

package replay

// peakRSSBytes 在不支持的平台上返回 0。
func peakRSSBytes() int64 { return 0 }
