package windows

// network.go registers Windows networking and socket DLLs.

func init() {
	// ── ws2_32 ────────────────────────────────────────────────────────────────
	// Windows Sockets 2: socket, bind, connect, send, recv, WSAStartup, etc.
	// Required by any networking code. Always the first network lib to link.
	register("ws2_32", Entry{
		ImportLib: "ws2_32.lib",
		DLLName:   "WS2_32.dll",
		MinGWLib:  "libws2_32.a",
	})

	// ── mswsock ───────────────────────────────────────────────────────────────
	// Microsoft extension to WinSock: AcceptEx, ConnectEx,
	// TransmitFile, WSARecvMsg. Loaded at runtime via WSAIoctl
	// (SIO_GET_EXTENSION_FUNCTION_POINTER) but needs mswsock.lib at link.
	register("mswsock", Entry{
		ImportLib: "mswsock.lib",
		DLLName:   "MSWSOCK.dll",
		MinGWLib:  "libmswsock.a",
	})

	// ── dnsapi ────────────────────────────────────────────────────────────────
	// DNS client API: DnsQuery, DnsQueryEx, DnsFree, DnsNameCompare.
	register("dnsapi", Entry{
		ImportLib: "dnsapi.lib",
		DLLName:   "DNSAPI.dll",
		MinGWLib:  "libdnsapi.a",
	})

	// ── iphlpapi ──────────────────────────────────────────────────────────────
	// IP Helper: GetAdaptersAddresses, GetIfTable, GetIpNetTable,
	// GetTcpTable, NotifyIpInterfaceChange, ConvertInterfaceLuidToIndex.
	register("iphlpapi", Entry{
		ImportLib: "iphlpapi.lib",
		DLLName:   "IPHLPAPI.dll",
		MinGWLib:  "libiphlpapi.a",
	})

	// ── winhttp ───────────────────────────────────────────────────────────────
	// WinHTTP: WinHttpOpen, WinHttpConnect, WinHttpSendRequest.
	// Preferred over WinINet for services and non-interactive clients.
	register("winhttp", Entry{
		ImportLib: "winhttp.lib",
		DLLName:   "winhttp.dll",
		MinGWLib:  "libwinhttp.a",
	})

	// ── wininet ───────────────────────────────────────────────────────────────
	// WinINet: InternetOpen, InternetConnect, HttpOpenRequest.
	// Higher-level than WinHTTP; handles proxy, cookies, cache.
	// Avoid in services — use WinHTTP instead.
	register("wininet", Entry{
		ImportLib: "wininet.lib",
		DLLName:   "WININET.dll",
		MinGWLib:  "libwininet.a",
	})

	// ── urlmon ────────────────────────────────────────────────────────────────
	// URL Moniker: URLDownloadToFile, CoInternetSetFeatureEnabled,
	// security zone management. Used by IE/shell URL handling.
	register("urlmon", Entry{
		ImportLib: "urlmon.lib",
		DLLName:   "urlmon.dll",
		MinGWLib:  "liburlmon.a",
	})

	// ── wldap32 ───────────────────────────────────────────────────────────────
	// Windows LDAP client: ldap_init, ldap_bind_s, ldap_search_ext_s.
	register("wldap32", Entry{
		ImportLib: "wldap32.lib",
		DLLName:   "WLDAP32.dll",
		MinGWLib:  "libwldap32.a",
	})
}