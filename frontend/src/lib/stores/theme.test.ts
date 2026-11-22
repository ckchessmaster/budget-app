import { beforeEach, describe, expect, it, vi } from 'vitest';
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
