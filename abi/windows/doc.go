// Package windows provides system library resolution for WindowsSystemLib imports.
//
// # Import Libraries vs DLL Names
//
// On Windows the linker does not resolve system DLLs by path at link time.
// Instead it uses import libraries (.lib files) from the Windows SDK to build
// the IAT (Import Address Table). At runtime the loader finds the DLL by name
// in the standard search path (System32, SysWOW64, the app directory, PATH).
//
// Therefore Entry has two distinct fields:
//
//   - ImportLib  — the .lib filename the PE linker passes on the command line,
//                  e.g. "kernel32.lib". The driver resolves its full path from
//                  the active Windows SDK layout (um\x64\ or um\arm64\).
//
//   - DLLName    — the DLL filename written into the PE IAT, e.g. "KERNEL32.dll".
//                  This is what the Windows loader searches for at runtime.
//
// # SDK Layout
//
// The import libraries for user-mode code live at:
//
//	%ProgramFiles(x86)%\Windows Kits\10\Lib\<sdk-ver>\um\x64\<name>.lib   (amd64)
//	%ProgramFiles(x86)%\Windows Kits\10\Lib\<sdk-ver>\um\arm64\<name>.lib (arm64)
//
// CRT import libraries live at:
//
//	%ProgramFiles(x86)%\Windows Kits\10\Lib\<sdk-ver>\ucrt\x64\           (ucrt)
//	<MSVC>\VC\Tools\MSVC\<ver>\lib\x64\                                    (vcruntime)
//
// The driver is responsible for locating the active SDK version and
// constructing the full path. This package only provides the filenames.
//
// # Cross-Compilation
//
// When cross-compiling for Windows from Linux or macOS, the driver should
// look for the import libraries in a Wine prefix or a MinGW/LLVM sysroot.
// MinGW ships equivalent .a files that the ELF-based linker treats the same
// way (e.g. libkernel32.a → DLL "KERNEL32.dll").
package windows