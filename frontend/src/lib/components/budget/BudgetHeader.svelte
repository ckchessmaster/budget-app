<script lang="ts">
	import { theme } from '$lib/stores/theme.svelte';
	import WorkspaceSwitcher from '$lib/components/WorkspaceSwitcher.svelte';
	import type { MonthSummary } from '$lib/types/budget';
	import type { SolvencyReport } from '$lib/types/transaction';
	import SolvencyPopover from './SolvencyPopover.svelte';

	interface Props {
		currentMonth: string; // YYYY-MM
		summary: MonthSummary | null;
		solvency?: SolvencyReport | null;
		accountCount?: number;
		toReviewCount?: number;
		isOffline?: boolean;
		queuedCount?: number;
		syncing?: boolean;
		isDrawerOpen?: boolean;
		onSyncNow?: () => void;
		onMonthChange: (newMonth: string) => void;
		onOpenCatchUp?: () => void;
		onToggleTransactionsDrawer?: () => void;
		onSignOut?: () => void;
	}

	let {
		currentMonth,
		summary,
		solvency = null,
		accountCount,
		toReviewCount = 0,
		isOffline = false,
		queuedCount = 0,
		syncing = false,
		isDrawerOpen = false,
		onSyncNow,
		onMonthChange,
		onOpenCatchUp,
		onToggleTransactionsDrawer,
		onSignOut
	}: Props = $props();

	let thisMonth = $derived.by(() => {
		const now = new Date();
		return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`;
	});

	let monthLabel = $derived.by(() => {
		const [y, m] = currentMonth.split('-').map(Number);
		if (!y || !m) return currentMonth;
		return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString('en-US', {
			month: 'short',
			year: 'numeric',
			timeZone: 'UTC'
		});
	});

	function shiftMonth(delta: number) {
		const [y, m] = currentMonth.split('-').map(Number);
		const d = new Date(Date.UTC(y, m - 1 + delta, 1));
		const next = `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`;
		onMonthChange(next);
	}

	function fmt(n: number): string {
		return Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
	}

	let readyState = $derived.by(() => {
		const val = summary?.readyToAssign ?? 0;
		if (Math.abs(val) < 0.005) return 'balanced';
		return val > 0 ? 'unassigned' : 'overassigned';
	});
</script>

<header
	data-testid="budget-header"
	style="padding-top: env(safe-area-inset-top, 0px);"
	class="sticky top-0 z-30 w-full bg-surface border-b border-border shrink-0 select-none"
>
	<div class="max-w-[1440px] mx-auto px-3 sm:px-6 py-2 flex flex-wrap items-center justify-between gap-x-2 gap-y-2 lg:flex-nowrap lg:gap-4">
		<!-- Group 1: Brand & Month Selector -->
		<div class="order-1 flex items-center gap-2 sm:gap-3 min-w-0 shrink-0">
			<a href="/budget" class="flex items-center gap-2 shrink-0 group">
				<span class="w-7 h-7 rounded-lg bg-accent-primary text-accent-primary-text flex items-center justify-center font-bold text-sm transition-opacity group-hover:opacity-90">
					$
				</span>
				<span class="hidden sm:inline text-sm font-bold tracking-tight text-ink-primary">
					Budget App
				</span>
			</a>

			<WorkspaceSwitcher />

			<div class="inline-flex items-center rounded-lg border border-border bg-subtle p-0.5 shrink-0">
				<button
					type="button"
					aria-label="Previous month"
					data-testid="prev-month-btn"
					onclick={() => shiftMonth(-1)}
					class="rounded-md px-1.5 sm:px-2 py-0.5 text-xs text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
				>
					&lsaquo;
				</button>
				<span
					data-testid="month-label"
					class="text-xs font-semibold text-ink-primary px-1.5 sm:px-2 min-w-[74px] sm:min-w-[82px] text-center"
				>
					{monthLabel}
				</span>
				<button
					type="button"
					aria-label="Next month"
					data-testid="next-month-btn"
					onclick={() => shiftMonth(1)}
					class="rounded-md px-1.5 sm:px-2 py-0.5 text-xs text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
				>
					&rsaquo;
				</button>
			</div>

			{#if currentMonth !== thisMonth}
				<button
					type="button"
					onclick={() => onMonthChange(thisMonth)}
					class="hidden md:inline-flex rounded-lg border border-border bg-subtle hover:bg-surface-hover px-2 py-1 text-xs font-medium text-ink-secondary hover:text-ink-primary transition-colors"
				>
					Today
				</button>
			{/if}
		</div>

		<!-- Group 2: Left to Budget / Offline / Sync + Solvency & Catch Up -->
		<div class="order-3 lg:order-2 basis-full lg:basis-auto flex flex-wrap items-center justify-between gap-1.5 sm:gap-2 min-w-0 lg:flex-1">
			<div class="flex flex-wrap items-center gap-1.5 sm:gap-2 min-w-0">
				<div
					data-testid="ready-to-assign-banner"
					class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium shrink-0 {readyState === 'balanced'
						? 'bg-status-good-bg text-status-good'
						: readyState === 'unassigned'
							? 'bg-accent-tint text-accent-pop'
							: 'bg-status-danger-bg text-status-danger'}"
				>
					{#if readyState === 'balanced'}
						<span class="font-mono font-bold">$0.00</span>
						<span>left to budget</span>
					{:else if readyState === 'unassigned'}
						<span class="font-mono font-bold">${fmt(summary?.readyToAssign ?? 0)}</span>
						<span>left to budget</span>
					{:else}
						<span class="font-mono font-bold">-${fmt(summary?.readyToAssign ?? 0)}</span>
						<span>over-budgeted</span>
					{/if}
				</div>

				{#if isOffline}
					<span class="text-xs px-2 py-0.5 rounded-full bg-status-warn-bg text-status-warn font-medium">
						Offline
					</span>
				{/if}

				{#if queuedCount > 0 && onSyncNow}
					<button
						type="button"
						onclick={onSyncNow}
						disabled={syncing || isOffline}
						class="text-xs px-2.5 py-1 rounded-lg bg-accent-pop text-accent-primary-text font-semibold disabled:opacity-50 hover:opacity-90 transition-opacity"
					>
						{syncing ? 'Syncing...' : `Sync (${queuedCount})`}
					</button>
				{/if}
			</div>

			<div class="flex flex-wrap items-center gap-1.5 sm:gap-2 shrink-0">
				<SolvencyPopover {solvency} {accountCount} />

				{#if toReviewCount > 0}
					<button
						type="button"
						data-testid="catch-up-btn"
						onclick={() => onOpenCatchUp?.()}
						class="inline-flex items-center gap-1.5 rounded-lg bg-accent-pop text-accent-primary-text px-2.5 py-1.5 text-xs font-semibold hover:opacity-90 transition-opacity"
					>
						Catch Up ({toReviewCount})
					</button>
				{/if}
			</div>
		</div>

		<!-- Group 3: Accounts, Rules, Settings, Theme Toggle, Mobile Drawer Toggle, Sign Out -->
		<div class="order-2 lg:order-3 flex items-center gap-1.5 sm:gap-2 shrink-0 pr-0.5">
			<a
				href="/accounts"
				class="hidden md:inline-flex items-center px-2 py-1.5 rounded-lg text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
			>
				Accounts
			</a>
			<a
				href="/rules"
				class="hidden sm:inline-flex items-center px-2 py-1.5 rounded-lg text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
			>
				Rules
			</a>

			<a
				href="/settings"
				aria-label="Settings"
				title="Settings"
				data-testid="settings-nav-link"
				class="p-2 rounded-lg border border-border bg-subtle hover:bg-surface-hover text-ink-secondary hover:text-ink-primary transition-colors"
			>
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
					<circle cx="12" cy="12" r="3" />
					<path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
				</svg>
			</a>

			<button
				type="button"
				onclick={() => theme.toggle()}
				aria-label={theme.effective === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
				title={theme.effective === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
				data-testid="theme-toggle-btn"
				class="p-2 rounded-lg border border-border bg-subtle hover:bg-surface-hover text-ink-secondary hover:text-ink-primary transition-colors"
			>
				{#if theme.effective === 'dark'}
					<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<circle cx="12" cy="12" r="4" />
						<path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
					</svg>
				{:else}
					<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
					</svg>
				{/if}
			</button>

			<button
				type="button"
				data-testid="mobile-transactions-toggle"
				aria-label={isDrawerOpen ? 'Close transactions drawer' : 'Open transactions drawer'}
				aria-expanded={isDrawerOpen}
				aria-controls="mobile-transactions-drawer"
				onclick={() => onToggleTransactionsDrawer?.()}
				class="lg:hidden relative inline-flex items-center justify-center p-2 rounded-lg border border-border bg-subtle hover:bg-surface-hover text-ink-primary transition-colors"
			>
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
					<path d="M4 2v20l2-1 2 1 2-1 2 1 2-1 2 1 2-1 2 1V2l-2 1-2-1-2 1-2-1-2 1-2-1-2 1Z" />
					<path d="M8 7h8" />
					<path d="M8 11h8" />
					<path d="M8 15h5" />
				</svg>
				{#if toReviewCount > 0}
					<span class="absolute -top-1.5 -right-1.5 inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full text-[10px] font-bold bg-accent-pop text-accent-primary-text">
						{toReviewCount}
					</span>
				{/if}
			</button>

			{#if onSignOut}
				<button
					type="button"
					onclick={onSignOut}
					aria-label="Sign out"
					title="Sign out"
					class="hidden sm:inline-flex items-center px-2 py-1.5 rounded-lg text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
				>
					Sign Out
				</button>
			{/if}
		</div>
	</div>
</header>
