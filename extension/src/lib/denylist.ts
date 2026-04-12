export const DENY_DOMAINS: string[] = [
	'accounts.google.com',
	'mail.google.com',
	'docs.google.com',
	'drive.google.com',
	'myaccount.google.com',
	'login.microsoftonline.com',
	'outlook.live.com',
	'onedrive.live.com',
	// US banks
	'bankofamerica.com',
	'chase.com',
	'wellsfargo.com',
	'paypal.com',
	'venmo.com',
	'coinbase.com',
	'kraken.com',
	// Password managers
	'1password.com',
	'bitwarden.com',
	'lastpass.com',
	'dashlane.com',
	'keepass.info',
	// US government (specific domains; TLDs covered below)
	'healthcare.gov',
	'medicaid.gov',
	'irs.gov',
	'ssa.gov',
	// Brazilian banks and financial institutions (P1-09)
	'itau.com.br',
	'bradesco.com.br',
	'bradesconetempresa.b.br',
	'nubank.com.br',
	'santander.com.br',
	'bb.com.br',
	'caixa.gov.br',
	'bancobrasil.com.br',
	'inter.co',
	'bancointer.com.br',
	'sicoob.com.br',
	'sicredi.com.br',
	'safra.com.br',
	'btgpactual.com',
];

export const DENY_URL_PATTERNS: RegExp[] = [
	/\/login/i,
	/\/signin/i,
	/\/auth\//i,
	/\/oauth/i,
	/\/saml/i,
	/\/sso/i,
	/\/mfa/i,
	/\/2fa/i,
	/\/verify/i,
	/\/password/i,
	/\/reset-password/i,
	/\/checkout/i,
	/\/payment/i,
];

// Private/internal IP ranges and loopback
function isPrivateHostname(hostname: string): boolean {
	// Loopback & link-local
	if (hostname === 'localhost' || hostname === '::1') return true;
	if (hostname.startsWith('127.')) return true;
	if (hostname.startsWith('169.254.')) return true;

	// RFC1918: 10.0.0.0/8
	if (hostname.startsWith('10.')) return true;

	// RFC1918: 172.16.0.0/12 (172.16–172.31)
	const m172 = hostname.match(/^172\.(\d+)\./);
	if (m172) {
		const b = parseInt(m172[1], 10);
		if (b >= 16 && b <= 31) return true;
	}

	// RFC1918: 192.168.0.0/16
	if (hostname.startsWith('192.168.')) return true;

	// Internal TLDs
	if (hostname.endsWith('.local') || hostname === 'local') return true;
	if (hostname.endsWith('.internal') || hostname === 'internal') return true;
	if (hostname.endsWith('.lan') || hostname === 'lan') return true;

	// Kubernetes cluster patterns
	if (hostname.includes('.cluster.') || hostname.endsWith('.cluster'))
		return true;
	if (hostname.includes('.svc.') || hostname.endsWith('.svc')) return true;
	if (hostname.endsWith('.k8s') || hostname.includes('.k8s.')) return true;

	return false;
}

export function isDeniedDomain(hostname: string): boolean {
	if (isPrivateHostname(hostname)) return true;

	// .gov / .mil TLDs (US and BR)
	if (hostname.endsWith('.gov') || hostname === 'gov') return true;
	if (hostname.endsWith('.mil') || hostname === 'mil') return true;
	if (hostname.endsWith('.gov.br')) return true;
	if (hostname.endsWith('.mil.br')) return true;

	for (const denied of DENY_DOMAINS) {
		// Exact match
		if (hostname === denied) return true;
		// Suffix match (e.g. sub.bankofamerica.com)
		if (hostname.endsWith('.' + denied)) return true;
	}

	return false;
}

export function isDeniedUrl(url: string): boolean {
	let hostname: string;
	let pathname: string;
	try {
		const parsed = new URL(url);
		hostname = parsed.hostname;
		pathname = parsed.pathname;
	} catch {
		return true; // unparseable URLs are denied
	}

	if (isDeniedDomain(hostname)) return true;

	for (const pattern of DENY_URL_PATTERNS) {
		if (pattern.test(pathname)) return true;
	}

	return false;
}
