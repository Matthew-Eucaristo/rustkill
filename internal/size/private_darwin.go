package size

import (
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/unix"
)

// attrCmnExtPrivateSize is ATTR_CMNEXT_PRIVATESIZE from <sys/attr.h>: the
// bytes of a file not shared with an APFS clone or snapshot, which deleting
// it would free right away.
const attrCmnExtPrivateSize = 0x00000008

type attrList struct {
	bitmapCount uint16
	reserved    uint16
	common      uint32
	volume      uint32
	dir         uint32
	file        uint32
	fork        uint32 // extended common attributes with FSOPT_ATTR_CMN_EXTENDED
}

// privateSize asks APFS how much of a regular file is its own. ok is false
// when the filesystem cannot say (not APFS, older macOS), and callers then
// fall back to allocated blocks.
func privateSize(path string) (int64, bool) {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return 0, false
	}
	al := attrList{bitmapCount: unix.ATTR_BIT_MAP_COUNT, fork: attrCmnExtPrivateSize}
	var buf [16]byte // u_int32_t length, then off_t
	// x/sys has no getattrlist wrapper; on darwin Syscall6 goes through
	// libSystem's syscall(), so the deprecated raw number still works.
	//lint:ignore SA1019 no libSystem wrapper exists for getattrlist in x/sys.
	_, _, errno := unix.Syscall6(unix.SYS_GETATTRLIST, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), unix.FSOPT_NOFOLLOW|unix.FSOPT_ATTR_CMN_EXTENDED, 0)
	if errno != 0 || binary.LittleEndian.Uint32(buf[0:4]) < 12 {
		return 0, false
	}
	return int64(binary.LittleEndian.Uint64(buf[4:12])), true
}
