package fsutil

import "golang.org/x/sys/windows"

// FreeSpace returns available bytes on the filesystem holding path. Only
// the Windows worker build needs this to compile; the server runs on Unix.
func FreeSpace(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}
