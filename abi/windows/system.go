package windows

// system.go registers Windows system management, diagnostics, hardware
// access, and miscellaneous Win32 support DLLs.

func init() {
	// ── setupapi ──────────────────────────────────────────────────────────────
	// Device setup and driver installation: SetupDiGetDeviceRegistryProperty,
	// SetupDiEnumDeviceInfo. Required for hardware enumeration.
	register("setupapi", Entry{
		ImportLib: "setupapi.lib",
		DLLName:   "SETUPAPI.dll",
		MinGWLib:  "libsetupapi.a",
	})

	// ── cfgmgr32 ──────────────────────────────────────────────────────────────
	// Configuration Manager (Plug and Play): CM_Get_Device_ID,
	// CM_Get_DevNode_Registry_Property, CM_Register_Notification.
	register("cfgmgr32", Entry{
		ImportLib: "cfgmgr32.lib",
		DLLName:   "CFGMGR32.dll",
		MinGWLib:  "libcfgmgr32.a",
	})

	// ── dbghelp ───────────────────────────────────────────────────────────────
	// Debug helpers: StackWalk64, SymInitialize, MiniDumpWriteDump,
	// ImageNtHeader. Used for crash reporting and symbol loading.
	register("dbghelp", Entry{
		ImportLib: "dbghelp.lib",
		DLLName:   "dbghelp.dll",
		MinGWLib:  "libdbghelp.a",
	})

	// ── psapi ─────────────────────────────────────────────────────────────────
	// Process Status API: EnumProcesses, GetModuleFileNameEx,
	// GetProcessMemoryInfo. Forwarded into kernel32 since Windows 7.
	register("psapi", Entry{
		ImportLib: "psapi.lib",
		DLLName:   "PSAPI.dll",
		MinGWLib:  "libpsapi.a",
	})

	// ── pdh ───────────────────────────────────────────────────────────────────
	// Performance Data Helper: PdhOpenQuery, PdhAddCounter, PdhCollectQueryData.
	// Used to read Windows performance counters (CPU %, disk I/O, etc.).
	register("pdh", Entry{
		ImportLib: "pdh.lib",
		DLLName:   "pdh.dll",
		MinGWLib:  "libpdh.a",
	})

	// ── wevtapi ───────────────────────────────────────────────────────────────
	// Windows Event Log API (Vista+): EvtQuery, EvtSubscribe, EvtRender.
	// Supersedes the classic OpenEventLog / ReadEventLog API in advapi32.
	register("wevtapi", Entry{
		ImportLib:  "wevtapi.lib",
		DLLName:    "wevtapi.dll",
		MinVersion: 0x0600, // Vista
	})

	// ── userenv ───────────────────────────────────────────────────────────────
	// User environment: LoadUserProfile, GetUserProfileDirectory,
	// ExpandEnvironmentStringsForUser.
	register("userenv", Entry{
		ImportLib: "userenv.lib",
		DLLName:   "USERENV.dll",
		MinGWLib:  "libuserenv.a",
	})

	// ── wtsapi32 ──────────────────────────────────────────────────────────────
	// Windows Terminal Services API: WTSQuerySessionInformation,
	// WTSSendMessage, WTSEnumerateSessions. Remote Desktop / session info.
	register("wtsapi32", Entry{
		ImportLib: "wtsapi32.lib",
		DLLName:   "wtsapi32.dll",
		MinGWLib:  "libwtsapi32.a",
	})

	// ── powrprof ──────────────────────────────────────────────────────────────
	// Power management profiles: PowerReadACValue, CallNtPowerInformation,
	// SetSuspendState. Battery info, sleep, hibernate.
	register("powrprof", Entry{
		ImportLib: "powrprof.lib",
		DLLName:   "POWRPROF.dll",
		MinGWLib:  "libpowrprof.a",
	})

	// ── virtdisk ──────────────────────────────────────────────────────────────
	// Virtual Disk API: OpenVirtualDisk, AttachVirtualDisk, CreateVirtualDisk.
	// VHD/VHDX management.
	register("virtdisk", Entry{
		ImportLib:  "virtdisk.lib",
		DLLName:    "virtdisk.dll",
		MinGWLib:   "libvirtdisk.a",
		MinVersion: 0x0601, // Windows 7
	})

	// ── onecore ───────────────────────────────────────────────────────────────
	// OneCore umbrella library: covers Win32 APIs common to all Windows 10+
	// device families (desktop, IoT, Xbox, HoloLens). Alternative to
	// linking kernel32+user32+... individually for universal targets.
	register("onecore", Entry{
		ImportLib:  "OneCore.lib",
		DLLName:    "", // umbrella — resolves into multiple DLLs at runtime
		MinVersion: 0x0A00, // Windows 10
	})

	// ── ntdll (secondary alias) ───────────────────────────────────────────────
	// Some callers request "nt" as shorthand.
	register("nt", Entry{
		ImportLib:  "ntdll.lib",
		DLLName:    "ntdll.dll",
		MinGWLib:   "libntdll.a",
		SystemOnly: true,
	})
}