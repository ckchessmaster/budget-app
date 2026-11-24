import { beforeEach, describe, expect, it, vi } from 'vitest';
import appCss from '../../app.css?raw';
import { theme } from './theme.svelte';

describe('theme store', () => {
	let systemMatches = false;
	let changeListeners: Array<(e: MediaQueryListEvent) => void> = [];
	let metaEl: HTMLMetaElement;

	beforeEach(() => {
		window.localStorage.clear();
		systemMatches = false;
		changeListeners = [];

		document.head.innerHTML = '';
		metaEl = document.createElement('meta');
		metaEl.name = 'color-scheme';
		metaEl.content = 'light dark';
		document.head.appendChild(metaEl);

		window.matchMedia = vi.fn().mockImplementation((query: string) => ({
			matches: query.includes('dark') ? systemMatches : false,
			media: query,
			onchange: null,
			addEventListener: (_type: string, cb: (e: MediaQueryListEvent) => void) => {
				changeListeners.push(cb);
			},
			removeEventListener: vi.fn(),
			addListener: vi.fn(),
			removeListener: vi.fn(),
			dispatchEvent: vi.fn()
		}));

		theme.refresh();
	});

	function emitSystemChange(matches: boolean) {
		systemMatches = matches;
		for (const cb of changeListeners) {
			cb({ matches } as MediaQueryListEvent);
		}
	}

	it('defaults to system preference when no override is stored', () => {
		expect(theme.override).toBeNull();
		expect(theme.effective).toBe('light');
		expect(metaEl.content).toBe('light dark');

		emitSystemChange(true);
		expect(theme.effective).toBe('dark');
		expect(theme.override).toBeNull();
		expect(metaEl.content).toBe('light dark');
	});

	it('stores literal override when toggling away from OS preference and keeps it across OS changes', () => {
		// OS starts light
		expect(theme.effective).toBe('light');

		// Toggle -> dark (differs from OS light, so persisted)
		theme.toggle();
		expect(theme.override).toBe('dark');
		expect(theme.effective).toBe('dark');
		expect(window.localStorage.getItem('color-scheme')).toBe('dark');
		expect(metaEl.content).toBe('dark');

		// OS changes to dark: stored override MUST NOT be removed
		emitSystemChange(true);
		expect(theme.override).toBe('dark');
		expect(window.localStorage.getItem('color-scheme')).toBe('dark');
		expect(metaEl.content).toBe('dark');

		// OS changes back to light: site stays dark
		emitSystemChange(false);
		expect(theme.override).toBe('dark');
		expect(theme.effective).toBe('dark');
		expect(metaEl.content).toBe('dark');
	});

	it('clears stored override and reverts to light dark when toggling back to match OS preference', () => {
		// OS is light, toggle to dark
		theme.toggle();
		expect(window.localStorage.getItem('color-scheme')).toBe('dark');
		expect(metaEl.content).toBe('dark');

		// Toggle again -> target is light, which matches OS light -> clears override
		theme.toggle();
		expect(theme.override).toBeNull();
		expect(theme.effective).toBe('light');
		expect(window.localStorage.getItem('color-scheme')).toBeNull();
		expect(metaEl.content).toBe('light dark');
	});

	it('supports explicit setMode(light | dark | system)', () => {
		theme.setMode('dark');
		expect(theme.override).toBe('dark');
		expect(theme.effective).toBe('dark');
		expect(window.localStorage.getItem('color-scheme')).toBe('dark');
		expect(metaEl.content).toBe('dark');

		theme.setMode('light');
		expect(theme.override).toBe('light');
		expect(theme.effective).toBe('light');
		expect(window.localStorage.getItem('color-scheme')).toBe('light');
		expect(metaEl.content).toBe('light');

		theme.setMode('system');
		expect(theme.override).toBeNull();
		expect(window.localStorage.getItem('color-scheme')).toBeNull();
		expect(metaEl.content).toBe('light dark');
	});
});

describe('Mono Cobalt & Obsidian CSS theme tokens', () => {
	const lightTokens: Record<string, string> = {
		'--bg-canvas': '#FAFAFA',
		'--bg-surface': '#FFFFFF',
		'--bg-subtle': '#F4F4F5',
		'--bg-hover': '#E4E4E7',
		'--border-default': '#E4E4E7',
		'--text-primary': '#09090B',
		'--text-secondary': '#52525B',
		'--text-muted': '#A1A1AA',
		'--accent-primary': '#09090B',
		'--accent-primary-text': '#FFFFFF',
		'--accent-pop': '#2563EB',
		'--accent-tint': '#EFF6FF',
		'--status-good': '#16A34A',
		'--status-good-bg': '#DCFCE7',
		'--status-warn': '#D97706',
		'--status-warn-bg': '#FEF3C7',
		'--status-danger': '#DC2626',
		'--status-danger-bg': '#FEE2E2'
	};

	const darkTokens: Record<string, string> = {
		'--bg-canvas': '#0B0D12',
		'--bg-surface': '#13161F',
		'--bg-subtle': '#1B202E',
		'--bg-hover': '#252C3F',
		'--border-default': '#232838',
		'--text-primary': '#F1F5F9',
		'--text-secondary': '#94A3B8',
		'--text-muted': '#64748B',
		'--accent-primary': '#60A5FA',
		'--accent-primary-text': '#0B0D12',
		'--accent-pop': '#60A5FA',
		'--accent-tint': 'rgba(96, 165, 250, 0.15)',
		'--status-good': '#34D399',
		'--status-good-bg': 'rgba(52, 211, 153, 0.14)',
		'--status-warn': '#FBBF24',
		'--status-warn-bg': 'rgba(251, 191, 36, 0.14)',
		'--status-danger': '#FB7185',
		'--status-danger-bg': 'rgba(251, 113, 133, 0.15)'
	};

	function extractTokens(block: string): Record<string, string> {
		const result: Record<string, string> = {};
		for (const match of block.matchAll(/(--[a-z-]+)\s*:\s*([^;]+);/g)) {
			result[match[1]] = match[2].trim();
		}
		return result;
	}

	it('defines all 18 Mono Cobalt light tokens on :root', () => {
		const rootMatch = appCss.match(/:root\s*\{([^}]*)\}/);
		expect(rootMatch).not.toBeNull();
		const tokens = extractTokens(rootMatch![1]);
		for (const [name, value] of Object.entries(lightTokens)) {
			expect(tokens[name]).toBe(value);
		}
		expect(Object.keys(tokens)).toHaveLength(18);
	});

	it('defines all 18 Obsidian dark tokens for prefers-color-scheme: dark without light override', () => {
		const mediaMatch = appCss.match(
			/@media\s*\(prefers-color-scheme:\s*dark\)\s*\{\s*:root:not\(:has\(>\s*head\s*>\s*meta\[name='color-scheme'\]\[content='light'\]\)\)\s*\{([^}]*)\}\s*\}/
		);
		expect(mediaMatch).not.toBeNull();
		const tokens = extractTokens(mediaMatch![1]);
		for (const [name, value] of Object.entries(darkTokens)) {
			expect(tokens[name]).toBe(value);
		}
		expect(Object.keys(tokens)).toHaveLength(18);
	});

	it('defines all 18 Obsidian dark tokens for explicit dark meta override', () => {
		const darkMatch = appCss.match(
			/:root:has\(>\s*head\s*>\s*meta\[name='color-scheme'\]\[content='dark'\]\)\s*\{([^}]*)\}/
		);
		expect(darkMatch).not.toBeNull();
		const tokens = extractTokens(darkMatch![1]);
		for (const [name, value] of Object.entries(darkTokens)) {
			expect(tokens[name]).toBe(value);
		}
		expect(Object.keys(tokens)).toHaveLength(18);
	});
});
