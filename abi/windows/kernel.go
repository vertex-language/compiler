package windows

// kernel.go registers the NT kernel and core Win32 execution layer libraries.
//
// These DLLs form the absolute foundation of every Windows process:
//
//   ntdll       — NT native API; the only DLL loaded by the kernel itself.
//                 All other DLLs depend on it. Never redistributable.
//   kernel32    — Win32 KERNEL subsystem: memory, files, processes, threads.
//                 Thin wrapper over ntdll / kernelbase.
//   kernelbase  — The real implementation behind kernel32 since Windows 7.
//                 Only link directly if you need APIs not in kernel32.
//   advapi32    — Advanced: registry, services, security, event log, crypto.

func init() {
	// ── ntdll ────────────────────────────────────────────────────────────────
	// The lowest user-mode DLL. Provides syscall stubs (NtXxx / ZwXxx),
	// the RTL runtime library, and the loader. Never link directly in
	// normal application code — use kernel32 or the Win32 API instead.
	register("ntdll", Entry{
		ImportLib:  "ntdll.lib",
		DLLName:    "ntdll.dll",
		MinGWLib:   "libntdll.a",
		SystemOnly: true,
	})

	// ── kernel32 ─────────────────────────────────────────────────────────────
	// The primary Win32 application API. Present on every Windows version.
	// Every console and GUI app links against kernel32.lib.
	register("kernel32", Entry{
		ImportLib: "kernel32.lib",
		DLLName:   "KERNEL32.dll",
		MinGWLib:  "libkernel32.a",
	})

	// ── kernelbase ───────────────────────────────────────────────────────────
	// Refactored implementation DLL introduced in Windows 7 (MinWin effort).
	// kernel32 forwards most calls here. Link directly only for APIs not
	// exposed through kernel32 (e.g. some AppModel, Package APIs).
	register("kernelbase", Entry{
		ImportLib:  "kernelbase.lib",
		DLLName:    "KERNELBASE.dll",
		MinGWLib:   "libkernelbase.a",
		SystemOnly: true,
		MinVersion: 0x0601, // Windows 7
	})

	// ── advapi32 ─────────────────────────────────────────────────────────────
	// Advanced Win32: registry (RegOpenKey), services (OpenSCManager),
	// security (AdjustTokenPrivileges), crypto (CryptAcquireContext),
	// event log (OpenEventLog), LSA policy.
	register("advapi32", Entry{
		ImportLib: "advapi32.lib",
		DLLName:   "ADVAPI32.dll",
		MinGWLib:  "libadvapi32.a",
	})
}