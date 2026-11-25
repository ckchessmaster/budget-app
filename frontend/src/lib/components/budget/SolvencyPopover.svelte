<script lang="ts">
	import type { SolvencyReport } from '$lib/types/transaction';

	interface Props {
		solvency: SolvencyReport | null;
		accountCount?: number;
	}

	let { solvency, accountCount }: Props = $props();

	let open = $state(false);
	let containerEl = $state<HTMLDivElement | null>(null);
	let triggerEl = $state<HTMLButtonElement | null>(null);

	function fmtSigned(n: number): string {
		const abs = Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
		return n < 0 ? `-$${abs}` : `$${abs}`;
	}

	$effect(() => {
		if (!open) return;

		function handlePointerDown(e: PointerEvent) {
			if (containerEl && e.target instanceof Node && !containerEl.contains(e.target)) {
				open = false;
			}
		}

		function handleKeyDown(e: KeyboardEvent) {
			if (e.key === 'Escape') {
				e.preventDefault();
				open = false;
				triggerEl?.focus();
			}
		}

		window.addEventListener('pointerdown', handlePointerDown);
		window.addEventListener('keydown', handleKeyDown);
		return () => {
			window.removeEventListener('pointerdown', handlePointerDown);
			window.removeEventListener('keydown', handleKeyDown);
		};
	});
</script>

{#if solvency}
	<div class="relative" bind:this={containerEl}>
		<button
			type="button"
			bind:this={triggerEl}
			data-testid="solvency-popover-trigger"
			aria-haspopup="dialog"
			aria-expanded={open}
			aria-controls="solvency-popover-panel"
			onclick={() => (open = !open)}
			class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors {solvency.isFloatingDebt
				? 'border-status-danger bg-status-danger-bg text-status-danger'
				: 'border-border bg-subtle hover:bg-surface-hover text-ink-primary'}"
		>
			<svg
				class="w-3.5 h-3.5 shrink-0 {solvency.isFloatingDebt ? 'text-status-danger' : 'text-status-good'}"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				stroke-linecap="round"
				stroke-linejoin="round"
				aria-hidden="true"
			>
				<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
			</svg>
			<span class="hidden sm:inline {solvency.isFloatingDebt ? 'text-status-danger' : 'text-ink-secondary'}">
				Net Cash
			</span>
			<span class="font-mono font-semibold">{fmtSigned(solvency.solvencyBuffer)}</span>
			<svg
				class="w-3 h-3 shrink-0 transition-transform {open ? 'rotate-180' : ''}"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				aria-hidden="true"
			>
				<path d="m6 9 6 6 6-6" />
			</svg>
		</button>

		{#if open}
			<div
				id="solvency-popover-panel"
				role="dialog"
				aria-label="Solvency breakdown"
				data-testid="solvency-popover-panel"
				class="fixed inset-x-3 mt-2 sm:absolute sm:inset-x-auto sm:right-0 sm:w-72 z-40 rounded-xl border border-border bg-surface shadow-lg p-3.5 text-xs text-ink-primary"
			>
				<div class="flex items-center justify-between gap-2 mb-2.5">
					<h3 class="font-semibold text-ink-primary">Solvency Breakdown</h3>
					{#if solvency.isFloatingDebt}
						<span class="rounded-full bg-status-danger-bg text-status-danger px-2 py-0.5 text-[10px] font-semibold">
							Floating Debt
						</span>
					{/if}
				</div>

				<div class="space-y-1.5">
					<div class="flex items-center justify-between gap-2">
						<span class="text-ink-secondary">Liquid Cash</span>
						<span class="font-mono font-semibold text-status-good">
							{fmtSigned(solvency.totalLiquidCash)}
						</span>
					</div>
					<div class="flex items-center justify-between gap-2">
						<span class="text-ink-secondary">Credit Card Debt</span>
						<span class="font-mono font-semibold text-ink-primary">
							{fmtSigned(solvency.totalCreditCardDebt)}
						</span>
					</div>
					<div class="border-t border-border my-2"></div>
					<div class="flex items-center justify-between gap-2">
						<span class="font-medium text-ink-primary">Solvency Buffer</span>
						<span
							class="font-mono font-bold {solvency.isFloatingDebt
								? 'text-status-danger'
								: 'text-status-good'}"
						>
							{fmtSigned(solvency.solvencyBuffer)}
						</span>
					</div>
				</div>

				<div class="border-t border-border mt-3 pt-2.5">
					<a
						href="/accounts"
						class="inline-flex items-center gap-1 text-xs font-semibold text-accent-pop hover:underline"
					>
						Manage Accounts{typeof accountCount === 'number' ? ` (${accountCount})` : ''} &rarr;
					</a>
				</div>
			</div>
		{/if}
	</div>
{/if}
