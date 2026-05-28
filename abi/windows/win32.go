package windows

// win32.go registers the core Win32 subsystem DLLs: UI, graphics, shell,
// COM, RPC, and the common control / dialog libraries.

func init() {
	// ── user32 ───────────────────────────────────────────────────────────────
	// Win32 USER subsystem: windows, menus, controls, messages, cursors,
	// keyboard/mouse input. Required by every GUI application.
	register("user32", Entry{
		ImportLib: "user32.lib",
		DLLName:   "USER32.dll",
		MinGWLib:  "libuser32.a",
	})

	// ── gdi32 ────────────────────────────────────────────────────────────────
	// Graphics Device Interface: drawing primitives, fonts, bitmaps, DCs.
	// Not needed for console-only apps.
	register("gdi32", Entry{
		ImportLib: "gdi32.lib",
		DLLName:   "GDI32.dll",
		MinGWLib:  "libgdi32.a",
	})

	// ── shell32 ──────────────────────────────────────────────────────────────
	// Windows Shell: SHGetKnownFolderPath, ShellExecute, drag-drop, icons,
	// file operation dialogs (SHFileOperation / IFileOperation).
	register("shell32", Entry{
		ImportLib: "shell32.lib",
		DLLName:   "SHELL32.dll",
		MinGWLib:  "libshell32.a",
	})

	// ── shlwapi ──────────────────────────────────────────────────────────────
	// Shell Lightweight API: path manipulation (PathCombine, PathFileExists),
	// string helpers, registry wrappers.
	register("shlwapi", Entry{
		ImportLib: "shlwapi.lib",
		DLLName:   "SHLWAPI.dll",
		MinGWLib:  "libshlwapi.a",
	})

	// ── ole32 ────────────────────────────────────────────────────────────────
	// COM: CoInitialize, CoCreateInstance, CoMarshalInterface, monikers,
	// structured storage, clipboard via OLE.
	register("ole32", Entry{
		ImportLib: "ole32.lib",
		DLLName:   "ole32.dll",
		MinGWLib:  "libole32.a",
	})

	// ── oleaut32 ─────────────────────────────────────────────────────────────
	// OLE Automation: BSTR, VARIANT, IDispatch, SafeArray, type libraries.
	register("oleaut32", Entry{
		ImportLib: "oleaut32.lib",
		DLLName:   "OLEAUT32.dll",
		MinGWLib:  "liboleaut32.a",
	})

	// ── rpcrt4 ───────────────────────────────────────────────────────────────
	// RPC runtime: RpcStringBindingCompose, RpcBindingFromStringBinding,
	// UUID generation (UuidCreate). Required by COM / DCOM.
	register("rpcrt4", Entry{
		ImportLib: "rpcrt4.lib",
		DLLName:   "RPCRT4.dll",
		MinGWLib:  "librpcrt4.a",
	})

	// ── comctl32 ─────────────────────────────────────────────────────────────
	// Common Controls v6: ListView, TreeView, Toolbar, Progress, Tab, etc.
	// Requires a manifest with comctl32 v6 dependency for themed controls.
	register("comctl32", Entry{
		ImportLib: "comctl32.lib",
		DLLName:   "COMCTL32.dll",
		MinGWLib:  "libcomctl32.a",
	})

	// ── comdlg32 ─────────────────────────────────────────────────────────────
	// Common Dialogs: GetOpenFileName, GetSaveFileName, ChooseFont, etc.
	register("comdlg32", Entry{
		ImportLib: "comdlg32.lib",
		DLLName:   "COMDLG32.dll",
		MinGWLib:  "libcomdlg32.a",
	})

	// ── pathcch ───────────────────────────────────────────────────────────────
	// Safe path manipulation API (PathCchCombine, PathCchAppend).
	// Supersedes shlwapi path functions. Windows 8+ only.
	register("pathcch", Entry{
		ImportLib:  "pathcch.lib",
		DLLName:    "KERNELBASE.dll", // forwarded into KernelBase since Win8
		MinGWLib:   "libpathcch.a",
		MinVersion: 0x0602, // Windows 8
	})

	// ── imm32 ────────────────────────────────────────────────────────────────
	// Input Method Manager: IME support for CJK text input.
	register("imm32", Entry{
		ImportLib: "imm32.lib",
		DLLName:   "IMM32.dll",
		MinGWLib:  "libimm32.a",
	})

	// ── uuid ─────────────────────────────────────────────────────────────────
	// Interface ID (IID) / CLSID link-time constants. Not a runtime DLL —
	// uuid.lib is a static archive of GUID definitions. DLLName is empty.
	register("uuid", Entry{
		ImportLib: "uuid.lib",
		DLLName:   "", // static only — no corresponding runtime DLL
		MinGWLib:  "",
	})

	// ── version ──────────────────────────────────────────────────────────────
	// File version info: GetFileVersionInfo, VerQueryValue, VS_FIXEDFILEINFO.
	register("version", Entry{
		ImportLib: "version.lib",
		DLLName:   "VERSION.dll",
		MinGWLib:  "libversion.a",
	})
}