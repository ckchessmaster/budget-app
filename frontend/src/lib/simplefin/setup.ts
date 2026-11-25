import type { LinkAccountMapping } from '$lib/api/simplefin';

export const SIMPLEFIN_CREATE_TOKEN_URL = 'https://beta-bridge.simplefin.org/simplefin/create';
export const SIMPLEFIN_BRIDGE_HOME_URL = 'https://beta-bridge.simplefin.org/';
export const SIMPLEFIN_MY_ACCOUNT_URL = 'https://beta-bridge.simplefin.org/my-account';

export type TokenValidationErrorReason =
	| 'empty'
	| 'looks_like_url'
	| 'looks_like_access_url'
	| 'invalid_base64'
	| 'invalid_claim_url';

export type TokenValidationResult =
	| { valid: true; token: string }
	| { valid: false; reason: TokenValidationErrorReason; message: string };

const BASE64_PATTERN = /^[A-Za-z0-9+/]+={0,2}$/;

/**
 * Validate a pasted SimpleFIN setup token before sending it to the claim API.
 * Catches common mistakes (pasting a bridge URL, pasting an access URL, or truncated base64).
 */
export function validateSimpleFINSetupToken(raw: string): TokenValidationResult {
	const trimmed = raw.trim();
	if (!trimmed) {
		return {
			valid: false,
			reason: 'empty',
			message: 'Paste your SimpleFIN Setup Token to continue.'
		};
	}

	if (/^https?:\/\//i.test(trimmed)) {
		try {
			const parsed = new URL(trimmed);
			if (parsed.username || parsed.password) {
				return {
					valid: false,
					reason: 'looks_like_access_url',
					message:
						'That looks like a SimpleFIN Access URL rather than a Setup Token. Create a new Setup Token on SimpleFIN Bridge instead.'
				};
			}
		} catch {
			// Fall through to generic URL message
		}
		return {
			valid: false,
			reason: 'looks_like_url',
			message:
				'That looks like a web link rather than a Setup Token. Copy the encoded Setup Token shown after clicking New Connection on SimpleFIN Bridge.'
		};
	}

	const normalized = raw.replace(/\s+/g, '');
	if (normalized.length % 4 !== 0 || !BASE64_PATTERN.test(normalized)) {
		return {
			valid: false,
			reason: 'invalid_base64',
			message:
				"That doesn't look like a valid SimpleFIN Setup Token. Make sure you copied the entire token string from SimpleFIN Bridge."
		};
	}

	let decoded: string;
	try {
		decoded = atob(normalized).trim();
	} catch {
		return {
			valid: false,
			reason: 'invalid_base64',
			message:
				"That doesn't look like a valid SimpleFIN Setup Token. Make sure you copied the entire token string from SimpleFIN Bridge."
		};
	}

	try {
		const parsed = new URL(decoded);
		if ((parsed.protocol !== 'http:' && parsed.protocol !== 'https:') || !parsed.hostname) {
			return {
				valid: false,
				reason: 'invalid_claim_url',
				message:
					"That token doesn't decode to a valid SimpleFIN claim URL. Double-check that you copied the Setup Token from SimpleFIN Bridge."
			};
		}
		if (parsed.username || parsed.password) {
			return {
				valid: false,
				reason: 'looks_like_access_url',
				message:
					'That looks like a SimpleFIN Access URL rather than a Setup Token. Create a new Setup Token on SimpleFIN Bridge instead.'
			};
		}
	} catch {
		return {
			valid: false,
			reason: 'invalid_claim_url',
			message:
				"That token doesn't decode to a valid SimpleFIN claim URL. Double-check that you copied the Setup Token from SimpleFIN Bridge."
		};
	}

	return { valid: true, token: normalized };
}

/**
 * Translate low-level API / Temporal claim or discover failures into actionable user guidance.
 */
export function formatSimpleFINClaimError(
	rawMessage: string,
	context: 'claim' | 'discover' = 'claim'
): string {
	const lower = rawMessage.toLowerCase();
	if (lower.includes('simplefin_not_connected')) {
		return 'No active SimpleFIN connection was found. Reconnect with a new setup token below.';
	}
	if (
		lower.includes('simplefin rejected the credentials') ||
		lower.includes('simplefin claim rejected') ||
		lower.includes('simplefin auth error') ||
		lower.includes('invalid simplefin setup token')
	) {
		if (context === 'discover') {
			return 'SimpleFIN rejected your saved connection — it may have been revoked on SimpleFIN Bridge. Reconnect with a new setup token below.';
		}
		return 'SimpleFIN rejected that setup token — it may be expired or already used. Setup tokens are single-use, so generate a fresh one on SimpleFIN Bridge and try again.';
	}
	if (lower.includes('timed out')) {
		return 'SimpleFIN took too long to respond. Your bank may be slow right now — check SimpleFIN Bridge or try again in a moment.';
	}
	if (lower.includes('setup_token is required')) {
		return 'Paste a SimpleFIN Setup Token before continuing.';
	}
	return (
		rawMessage ||
		(context === 'discover'
			? 'Failed to fetch SimpleFIN accounts. Please try again.'
			: 'Failed to claim SimpleFIN token. Please try again.')
	);
}

/**
 * Best-effort default account type inferred from the bank account's display name.
 */
export function inferAccountType(name: string): LinkAccountMapping['type'] {
	if (
		/\b(credit|card|visa|mastercard|amex|american express|discover|sapphire|freedom|venture|quicksilver|savor|platinum|gold card)\b/i.test(
			name
		)
	) {
		return 'credit_card';
	}
	if (/\b(saving|savings|hysa|money market|reserve|cd)\b/i.test(name)) {
		return 'savings';
	}
	if (
		/\b(401k|401\(k\)|ira|roth|brokerage|invest|investment|vanguard|fidelity|schwab|etrade|robinhood|wealthfront|betterment)\b/i.test(
			name
		)
	) {
		return 'investment';
	}
	if (/\b(loan|mortgage|auto|car note|student|heloc|line of credit)\b/i.test(name)) {
		return 'loan';
	}
	if (/\b(cash|wallet|petty)\b/i.test(name)) {
		return 'cash';
	}
	return 'checking';
}
