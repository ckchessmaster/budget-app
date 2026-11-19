export type ThemeScheme = 'light' | 'dark';
export type ThemeOverride = ThemeScheme | null;

const STORAGE_KEY = 'color-scheme';

function readStoredOverride(): ThemeOverride {
	if (typeof window === 'undefined') return null;
	try {
		const raw = window.localStorage.getItem(STORAGE_KEY);
		if (raw === 'light' || raw === 'dark') return raw;
	} catch {
		// localStorage unavailable
	}
	return null;
}

function readSystemDark(): boolean {
	if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
		return false;
	}
	return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function syncMeta(next: ThemeOverride) {
	if (typeof document === 'undefined') return;
	const meta = document.querySelector('meta[name="color-scheme"]');
	if (meta) {
		meta.setAttribute('content', next ?? 'light dark');
	}
}

class ThemeStore {
	override = $state<ThemeOverride>(null);
	systemDark = $state<boolean>(false);
	private mqAttached = false;
	private storageAttached = false;

	constructor() {
		if (typeof window !== 'undefined') {
			this.init();
		}
	}

	/** Current effective color scheme rendered to the user. */
	get effective(): ThemeScheme {
		if (this.override) return this.override;
		return this.systemDark ? 'dark' : 'light';
	}

	/** Initialize listeners and hydrate from DOM/localStorage; safe to call multiple times. */
	init() {
		if (typeof window === 'undefined') return;
		this.override = readStoredOverride();
		this.systemDark = readSystemDark();
		syncMeta(this.override);

		if (!this.mqAttached && typeof window.matchMedia === 'function') {
			this.mqAttached = true;
			const mq = window.matchMedia('(prefers-color-scheme: dark)');
			const onChange = (e: MediaQueryListEvent) => {
				// Never clear a stored override when the OS theme happens to match it.
				this.systemDark = e.matches;
			};
			if (typeof mq.addEventListener === 'function') {
				mq.addEventListener('change', onChange);
			}
		}

		if (!this.storageAttached) {
			this.storageAttached = true;
			window.addEventListener('storage', (e: StorageEvent) => {
				if (e.key === STORAGE_KEY || e.key === null) {
					this.override = readStoredOverride();
					syncMeta(this.override);
				}
			});
		}
	}

	/** Re-reads state from storage and matchMedia (useful in tests). */
	refresh() {
		this.mqAttached = false;
		this.init();
	}

	/**
	 * Two-state toggle: flips to the opposite of what is currently rendered.
	 * If the target differs from the system preference, persists it; if it matches
	 * the system preference, clears the override so the site follows OS changes again.
	 */
	toggle() {
		const currentRendered = this.effective;
		const target: ThemeScheme = currentRendered === 'dark' ? 'light' : 'dark';
		const system: ThemeScheme = this.systemDark ? 'dark' : 'light';

		if (target !== system) {
			this.override = target;
			try {
				window.localStorage.setItem(STORAGE_KEY, target);
			} catch {
				// Ignore storage failures, still apply to document
			}
		} else {
			this.override = null;
			try {
				window.localStorage.removeItem(STORAGE_KEY);
			} catch {
				// Ignore storage failures
			}
		}
		syncMeta(this.override);
	}
}

export const theme = new ThemeStore();
