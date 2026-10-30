package envcheck

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                        = syscall.NewLazyDLL("kernel32.dll")
	procGetDynamicTimeZoneInfo      = kernel32.NewProc("GetDynamicTimeZoneInformation")
	procGetUserDefaultLocaleName    = kernel32.NewProc("GetUserDefaultLocaleName")
	procGetUserPreferredUILanguages = kernel32.NewProc("GetUserPreferredUILanguages")
)

// dynamicTimeZoneInformation is DYNAMIC_TIME_ZONE_INFORMATION.
type dynamicTimeZoneInformation struct {
	Bias                        int32
	StandardName                [32]uint16
	StandardDate                [8]uint16
	StandardBias                int32
	DaylightName                [32]uint16
	DaylightDate                [8]uint16
	DaylightBias                int32
	TimeZoneKeyName             [128]uint16
	DynamicDaylightTimeDisabled uint8
}

func read() Env {
	var e Env
	_, e.Offset = time.Now().Zone()
	if e.TimeZone = os.Getenv("TZ"); e.TimeZone == "" {
		var tz dynamicTimeZoneInformation
		if r, _, _ := procGetDynamicTimeZoneInfo.Call(uintptr(unsafe.Pointer(&tz))); r != 0xFFFFFFFF {
			e.TimeZone = syscall.UTF16ToString(tz.TimeZoneKeyName[:])
		}
	}
	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	if n, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n != 0 {
		e.Locale = syscall.UTF16ToString(buf)
	}
	e.Languages = uiLanguages()
	return e
}

// uiLanguages are the user's display languages, first first.
func uiLanguages() []string {
	const muiLanguageName = 0x8
	var count, size uint32
	if r, _, _ := procGetUserPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&size))); r == 0 || size == 0 {
		return nil
	}
	buf := make([]uint16, size)
	if r, _, _ := procGetUserPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return nil
	}
	// A list of strings, each ending in a NUL, then one more NUL.
	var langs []string
	for start, i := 0, 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i > start {
				langs = append(langs, syscall.UTF16ToString(buf[start:i]))
			}
			start = i + 1
		}
	}
	return langs
}
