package ui

import (
	"syscall"
	"unsafe"
)

// systemPowerStatus es SYSTEM_POWER_STATUS de Windows.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

var getSystemPowerStatus = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// systemBattery lee la batería con GetSystemPowerStatus: el nivel (0..1) y si
// está cargando. ok es false en un equipo sin batería.
func systemBattery() (level float64, charging, ok bool) {
	var st systemPowerStatus
	if r, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return 0, false, false
	}
	// 128: sin batería; 255: desconocido.
	if st.BatteryFlag&128 != 0 || st.BatteryLifePercent > 100 {
		return 0, false, false
	}
	return float64(st.BatteryLifePercent) / 100, st.ACLineStatus == 1, true
}
