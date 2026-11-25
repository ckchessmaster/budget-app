import { describe, expect, it } from 'vitest';
import {
	formatSimpleFINClaimError,
	inferAccountType,
	validateSimpleFINSetupToken
} from './setup';

describe('validateSimpleFINSetupToken', () => {
	it('accepts a valid base64 HTTPS claim URL and strips whitespace', () => {
		const claimUrl = 'https://beta-bridge.simplefin.org/simplefin/claim/abc123';
		const encoded = btoa(claimUrl);
		const withWhitespace = `  ${encoded.slice(0, 12)}\n  ${encoded.slice(12)}  `;

		const res = validateSimpleFINSetupToken(withWhitespace);
		expect(res).toEqual({ valid: true, token: encoded });
	});

	it('rejects empty or whitespace-only input', () => {
		const res = validateSimpleFINSetupToken('   \n ');
		expect(res.valid).toBe(false);
		if (!res.valid) {
			expect(res.reason).toBe('empty');
		}
	});

	it('catches a plain SimpleFIN web URL pasted instead of a token', () => {
		const res = validateSimpleFINSetupToken('https://beta-bridge.simplefin.org/simplefin/create');
		expect(res.valid).toBe(false);
		if (!res.valid) {
			expect(res.reason).toBe('looks_like_url');
			expect(res.message).toContain('web link');
		}
	});

	it('catches an access URL with embedded credentials (raw or base64-encoded)', () => {
		const rawAccessUrl = 'https://user123:secretpass@beta-bridge.simplefin.org/simplefin';
		const rawRes = validateSimpleFINSetupToken(rawAccessUrl);
		expect(rawRes.valid).toBe(false);
		if (!rawRes.valid) {
			expect(rawRes.reason).toBe('looks_like_access_url');
		}

		const encodedRes = validateSimpleFINSetupToken(btoa(rawAccessUrl));
		expect(encodedRes.valid).toBe(false);
		if (!encodedRes.valid) {
			expect(encodedRes.reason).toBe('looks_like_access_url');
		}
	});

	it('rejects malformed non-base64 text', () => {
		const res = validateSimpleFINSetupToken('not a valid token!!!');
		expect(res.valid).toBe(false);
		if (!res.valid) {
			expect(res.reason).toBe('invalid_base64');
		}
	});

	it('rejects base64 text that does not decode to an HTTP(S) URL', () => {
		const res = validateSimpleFINSetupToken(btoa('just some random text'));
		expect(res.valid).toBe(false);
		if (!res.valid) {
			expect(res.reason).toBe('invalid_claim_url');
		}
	});
});

describe('formatSimpleFINClaimError', () => {
	it('explains single-use expiration when SimpleFIN rejects credentials', () => {
		const msg = formatSimpleFINClaimError('simplefin rejected the credentials');
		expect(msg).toContain('single-use');
		expect(msg).toContain('expired or already used');
	});

	it('explains revoked saved connection when discover rejects credentials', () => {
		const msg = formatSimpleFINClaimError('simplefin rejected the credentials', 'discover');
		expect(msg).toContain('revoked');
		expect(msg).not.toContain('single-use');
	});

	it('explains missing connection when simplefin_not_connected is returned', () => {
		const msg = formatSimpleFINClaimError('simplefin_not_connected', 'discover');
		expect(msg).toContain('No active SimpleFIN connection');
	});

	it('translates timeout messages into user-friendly guidance', () => {
		const msg = formatSimpleFINClaimError('Timed out waiting for the SimpleFIN claim to finish');
		expect(msg).toContain('took too long');
	});

	it('passes through unrecognized error messages', () => {
		expect(formatSimpleFINClaimError('network offline')).toBe('network offline');
	});
});

describe('inferAccountType', () => {
	it('infers account types from common bank account names', () => {
		expect(inferAccountType('Chase Sapphire Preferred')).toBe('credit_card');
		expect(inferAccountType('Amex Gold Card')).toBe('credit_card');
		expect(inferAccountType('Ally High-Yield Savings')).toBe('savings');
		expect(inferAccountType('Vanguard Roth IRA')).toBe('investment');
		expect(inferAccountType('SoFi Auto Loan')).toBe('loan');
		expect(inferAccountType('Petty Cash')).toBe('cash');
		expect(inferAccountType('Everyday Checking')).toBe('checking');
	});
});
