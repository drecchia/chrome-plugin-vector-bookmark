export const DENY_DOMAINS: string[] = [
	'accounts.google.com',
	'mail.google.com',
	'docs.google.com',
	'drive.google.com',
	'myaccount.google.com',
	'login.microsoftonline.com',
	'outlook.live.com',
	'onedrive.live.com',
	'bankofamerica.com',
	'chase.com',
	'wellsfargo.com',
	'paypal.com',
	'venmo.com',
	'coinbase.com',
	'kraken.com',
	'1password.com',
	'bitwarden.com',
	'lastpass.com',
	'dashlane.com',
	'keepass.info',
	'healthcare.gov',
	'medicaid.gov',
	'irs.gov',
	'ssa.gov',
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

export function isDeniedDomain(hostname: string): boolean {
	// .gov TLD
	if (hostname.endsWith('.gov') || hostname === 'gov') return true;
	// .mil TLD
	if (hostname.endsWith('.mil') || hostname === 'mil') return true;

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
