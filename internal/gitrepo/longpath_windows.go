package gitrepo

import "syscall"

func longPath(path string) string {
	short, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buffer := make([]uint16, 512)
	for {
		n, err := syscall.GetLongPathName(short, &buffer[0], uint32(len(buffer)))
		if err != nil || n == 0 {
			return path
		}
		if int(n) < len(buffer) {
			return syscall.UTF16ToString(buffer[:n])
		}
		buffer = make([]uint16, n+1)
	}
}
