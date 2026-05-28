package windows

// crt.go registers the Visual C++ and Universal C Runtime libraries.
//
// The CRT split was introduced with Visual Studio 2015:
//
//   ucrt        — Universal CRT (ucrtbase.dll). Ships as part of Windows 10+
//                 and is available via Windows Update on Vista–8.1. Contains
//                 the C standard library (stdio, string, math, etc.).
//
//   vcruntime   — Visual C++ runtime helpers (vcruntime140.dll or later).
//                 Exception handling (__CxxFrameHandler), RTTI, SEH glue.
//                 Must be redistributed alongside the application.
//
//   msvcrt      — The legacy CRT DLL (msvcrt.dll) that ships with Windows.
//                 Do NOT link new code against msvcrt.lib — it is an OS
//                 component not intended as a public ABI. Use ucrt instead.
//
// For the Vertex compiler, wasm IR should import "windows/ucrt" for standard
// C library functions and "windows/vcruntime" for C++ runtime support.

func init() {
	// ── ucrt ─────────────────────────────────────────────────────────────────
	// The Universal C Runtime. Link ucrt.lib (dynamic) or libucrt.lib (static).
	// ucrtbase.dll ships inbox on Windows 10+; available via Windows Update
	// on Vista through 8.1. The safe default for all new Windows code.
	register("ucrt", Entry{
		ImportLib: "ucrt.lib",
		DLLName:   "ucrtbase.dll",
		MinGWLib:  "libucrt.a",
		MinVersion: 0x0600, // Vista (via Windows Update)
	})

	// ── ucrt (static variant alias) ──────────────────────────────────────────
	// Some callers request "libucrt" or "ucrtbase" directly.
	register("ucrtbase", Entry{
		ImportLib: "ucrt.lib",
		DLLName:   "ucrtbase.dll",
		MinGWLib:  "libucrt.a",
	})

	// ── vcruntime ────────────────────────────────────────────────────────────
	// Visual C++ runtime: SEH, C++ exceptions, RTTI. The exact DLL name
	// depends on the toolset version (140 = VS2015-2022). vcruntime.lib
	// in the SDK links the version-appropriate DLL automatically.
	register("vcruntime", Entry{
		ImportLib: "vcruntime.lib",
		DLLName:   "vcruntime140.dll",
		MinGWLib:  "",
		MinVersion: 0x0600,
	})

	// ── msvcrt ───────────────────────────────────────────────────────────────
	// Legacy OS CRT. Ships with Windows since NT 4. Used internally by
	// system DLLs. Not a supported public API surface for new applications.
	// Registered here so the driver can emit a clear warning if wasm IR
	// imports it directly, recommending ucrt instead.
	register("msvcrt", Entry{
		ImportLib:  "msvcrt.lib",
		DLLName:    "msvcrt.dll",
		MinGWLib:   "libmsvcrt.a",
		SystemOnly: true,
	})
}