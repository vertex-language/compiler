package darwin

// frameworks.go registers the core Apple system frameworks.
//
// Frameworks use a different install name path than flat dylibs:
//
//	/System/Library/Frameworks/<Name>.framework/Versions/A/<Name>
//
// On macOS 11+, these are also in the dyld shared cache and the paths
// below will not exist as files on disk, but they remain the correct
// LC_LOAD_DYLIB values to embed in the Mach-O binary.
//
// Wasm IR imports these via the "darwin/framework/<Name>" module path:
//
//	(import "darwin/framework/CoreFoundation" "CFStringCreateWithCString@ptr.i32.ptr" (func ...))

const fwPrefix = "/System/Library/Frameworks/"
const fwSuffix = ".framework/Versions/A/"
const sdkFWPrefix = "System/Library/Frameworks/"
const sdkFWSuffix = ".framework/"

func fw(name string) FrameworkEntry {
	return FrameworkEntry{
		InstallName: fwPrefix + name + fwSuffix + name,
		SDKStub:     sdkFWPrefix + name + sdkFWSuffix + name + ".tbd",
	}
}

func init() {
	// ── Core OS ──────────────────────────────────────────────────────────────

	// CoreFoundation: CFString, CFURL, CFArray, CFDictionary, run loops, etc.
	// The most fundamental Apple framework. Required for most Cocoa work.
	registerFramework("CoreFoundation", fw("CoreFoundation"))

	// CoreServices: FSEvents, Launch Services, Search Kit, etc.
	registerFramework("CoreServices", fw("CoreServices"))

	// Security: keychain, SecKey, SecCertificate, CommonCrypto, TLS.
	registerFramework("Security", fw("Security"))

	// SystemConfiguration: network reachability, interface info, dynamic store.
	registerFramework("SystemConfiguration", fw("SystemConfiguration"))

	// IOKit: hardware device access, I/O Registry, USB, HID, power management.
	registerFramework("IOKit", fw("IOKit"))

	// DiskArbitration: mount/unmount callbacks, disk appearance events.
	registerFramework("DiskArbitration", fw("DiskArbitration"))

	// ── Graphics & Media ─────────────────────────────────────────────────────

	// CoreGraphics: CGContext, CGImage, PDF rendering, display management.
	registerFramework("CoreGraphics", fw("CoreGraphics"))

	// CoreText: font selection, glyph layout, attributed string rendering.
	registerFramework("CoreText", fw("CoreText"))

	// CoreImage: GPU-accelerated image filters and processing.
	registerFramework("CoreImage", fw("CoreImage"))

	// ImageIO: image file decoding/encoding (JPEG, PNG, TIFF, HEIC, etc.).
	registerFramework("ImageIO", fw("ImageIO"))

	// CoreVideo: CVPixelBuffer, video display synchronisation (CVDisplayLink).
	registerFramework("CoreVideo", fw("CoreVideo"))

	// AVFoundation: audio/video capture, playback, editing, encoding.
	registerFramework("AVFoundation", fw("AVFoundation"))

	// Metal: GPU compute and rendering API. amd64 and arm64.
	registerFramework("Metal", fw("Metal"))

	// MetalKit: MTKView, MTKTextureLoader, mesh utilities.
	registerFramework("MetalKit", fw("MetalKit"))

	// ── Audio ────────────────────────────────────────────────────────────────

	// CoreAudio: AudioHardware, audio device enumeration, I/O.
	registerFramework("CoreAudio", fw("CoreAudio"))

	// AudioToolbox: AudioQueue, AudioFile, MIDI, extended audio format.
	registerFramework("AudioToolbox", fw("AudioToolbox"))

	// AudioUnit: plugin hosting and AU graph. Deprecated in favour of AVAudioEngine
	// but still widely used and fully present.
	registerFramework("AudioUnit", fw("AudioUnit"))

	// ── Networking ───────────────────────────────────────────────────────────

	// Network: NWConnection, NWListener, NWPathMonitor (modern network API).
	// Available since macOS 10.14; weak-link for 10.13 targets.
	registerFramework("Network", FrameworkEntry{
		InstallName: fwPrefix + "Network" + fwSuffix + "Network",
		SDKStub:     sdkFWPrefix + "Network" + sdkFWSuffix + "Network.tbd",
	})

	// CFNetwork: CFHTTPMessage, CFSocket, CFFTPD, URL loading built on CF.
	registerFramework("CFNetwork", fw("CFNetwork"))

	// ── AppKit / UI ──────────────────────────────────────────────────────────

	// AppKit: NSWindow, NSView, NSApplication — the macOS UI framework.
	// Not available on non-macOS Apple platforms.
	registerFramework("AppKit", fw("AppKit"))

	// Foundation: NSString, NSArray, NSData, NSRunLoop, GCD wrappers, etc.
	// The Objective-C/Swift base library; wraps CoreFoundation.
	registerFramework("Foundation", fw("Foundation"))

	// ── Developer / Debug ────────────────────────────────────────────────────

	// Accelerate: BLAS, LAPACK, vDSP, vImage — vectorised maths on Apple silicon.
	registerFramework("Accelerate", fw("Accelerate"))

	// Hypervisor: Apple Hypervisor.framework — hardware virtualisation API.
	// Apple silicon and Intel (Haswell+). Available since macOS 10.10.
	registerFramework("Hypervisor", fw("Hypervisor"))

	// vmnet: virtual network interfaces for VMs/containers. macOS 11+.
	registerFramework("vmnet", fw("vmnet"))
}