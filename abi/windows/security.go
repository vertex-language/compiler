package windows

// security.go registers Windows security, cryptography, and authentication DLLs.

func init() {
	// ── bcrypt ────────────────────────────────────────────────────────────────
	// Cryptography Next Generation (CNG): BCryptOpenAlgorithmProvider,
	// BCryptGenRandom, BCryptHash, BCryptEncrypt. The modern crypto API.
	// Available since Vista. Replaces CryptAPI for new code.
	register("bcrypt", Entry{
		ImportLib:  "bcrypt.lib",
		DLLName:    "bcrypt.dll",
		MinGWLib:   "libbcrypt.a",
		MinVersion: 0x0600, // Vista
	})

	// ── ncrypt ────────────────────────────────────────────────────────────────
	// CNG key storage: NCryptOpenKey, NCryptEncrypt, NCryptSignHash.
	// Smart card and TPM-backed key management. Requires bcrypt.
	register("ncrypt", Entry{
		ImportLib:  "ncrypt.lib",
		DLLName:    "ncrypt.dll",
		MinGWLib:   "libncrypt.a",
		MinVersion: 0x0600,
	})

	// ── crypt32 ───────────────────────────────────────────────────────────────
	// CryptoAPI: CertOpenStore, CertFindCertificateInStore, CryptSignMessage,
	// CryptVerifyMessageSignature. X.509 certificates, PKCS, CMS.
	register("crypt32", Entry{
		ImportLib: "crypt32.lib",
		DLLName:   "CRYPT32.dll",
		MinGWLib:  "libcrypt32.a",
	})

	// ── wintrust ──────────────────────────────────────────────────────────────
	// Authenticode: WinVerifyTrust, CryptCATAdminAcquireContext.
	// Verifies PE code signatures and catalog signatures.
	register("wintrust", Entry{
		ImportLib: "wintrust.lib",
		DLLName:   "WINTRUST.dll",
		MinGWLib:  "libwintrust.a",
	})

	// ── secur32 ───────────────────────────────────────────────────────────────
	// Security Support Provider Interface (SSPI): AcquireCredentialsHandle,
	// InitializeSecurityContext, AcceptSecurityContext. Kerberos, NTLM, TLS.
	register("secur32", Entry{
		ImportLib: "secur32.lib",
		DLLName:   "secur32.dll",
		MinGWLib:  "libsecur32.a",
	})

	// ── sspicli ───────────────────────────────────────────────────────────────
	// SSPI client-side: the actual implementation DLL behind secur32.
	// Link secur32.lib for the public API; link sspicli.lib only if you
	// need functions not forwarded through secur32.
	register("sspicli", Entry{
		ImportLib:  "sspicli.lib",
		DLLName:    "sspicli.dll",
		SystemOnly: true,
	})

	// ── ntmarta ───────────────────────────────────────────────────────────────
	// Access Control API implementation: SetNamedSecurityInfo,
	// GetNamedSecurityInfo, BuildExplicitAccessWithName.
	// Usually accessed via advapi32, not directly.
	register("ntmarta", Entry{
		ImportLib:  "ntmarta.lib",
		DLLName:    "ntmarta.dll",
		SystemOnly: true,
	})

	// ── cryptbase ─────────────────────────────────────────────────────────────
	// Low-level crypto primitives used by bcrypt/ncrypt internals.
	// Not a public API; registered for completeness.
	register("cryptbase", Entry{
		ImportLib:  "cryptbase.lib",
		DLLName:    "cryptbase.dll",
		SystemOnly: true,
	})
}