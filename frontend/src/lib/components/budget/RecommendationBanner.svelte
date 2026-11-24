<script lang="ts">
	import { updateMonthlyPlanned } from '$lib/api/budget';
	import type { SpendRecommendation } from '$lib/types/budget';

	interface Props {
		recommendations: SpendRecommendation[];
		yearMonth: string;
		onApply?: (categoryId: string, newPlanned: number) => Promise<void> | void;
		onApplied?: (recommendation: SpendRecommendation) => Promise<void> | void;
		onDismiss?: (categoryId: string) => void;
	}

	let {
		recommendations,
		yearMonth,
		onApply,
		onApplied,
		onDismiss
	}: Props = $props();

	let dismissedKeys = $state<string[]>([]);
	let applyingId = $state<string | null>(null);
	let errorMessage = $state('');
	let showAll = $state(false);

	function dismissKey(categoryId: string): string {
		return `${yearMonth}:${categoryId}`;
	}

	let activeRecommendations = $derived.by(() =>
		recommendations.filter(
			(rec) => rec.shouldPrompt && !dismissedKeys.includes(dismissKey(rec.categoryId))
		)
	);

	let visibleRecommendations = $derived(
		showAll ? activeRecommendations : activeRecommendations.slice(0, 2)
	);
	let hiddenCount = $derived(
		Math.max(0, activeRecommendations.length - visibleRecommendations.length)
	);

	function fmtCurrency(amount: number): string {
		const rounded = Math.round(amount * 100) / 100;
		const isWhole = Math.abs(rounded - Math.round(rounded)) < 0.005;
		const formatted = Math.abs(rounded).toLocaleString('en-US', {
			minimumFractionDigits: isWhole ? 0 : 2,
			maximumFractionDigits: isWhole ? 0 : 2
		});
		return rounded < 0 ? `-$${formatted}` : `$${formatted}`;
	}

	function deviationBadgeText(rec: SpendRecommendation): string {
		const pct = Math.round(rec.percentageDeviation * 100);
		const direction = rec.trailingMedianSpend < rec.currentPlanned ? 'below' : 'above';
		return `${pct}% ${direction} target`;
	}

	async function handleApply(rec: SpendRecommendation) {
		if (applyingId) return;
		applyingId = rec.categoryId;
		errorMessage = '';

		try {
			if (onApply) {
				await onApply(rec.categoryId, rec.trailingMedianSpend);
			} else {
				await updateMonthlyPlanned(rec.categoryId, yearMonth, rec.trailingMedianSpend);
			}
			await onApplied?.(rec);
		} catch (err: any) {
			errorMessage = err?.message || 'Failed to apply recommended target.';
		} finally {
			applyingId = null;
		}
	}

	function handleDismiss(categoryId: string) {
		const key = dismissKey(categoryId);
		if (!dismissedKeys.includes(key)) {
			dismissedKeys = [...dismissedKeys, key];
		}
		onDismiss?.(categoryId);
	}
</script>

{#if activeRecommendations.length > 0}
	<section
		aria-label="Budget trend recommendations"
		data-testid="recommendation-banner"
		class="rounded-xl border border-accent-pop/25 bg-accent-tint/60 px-4 py-3 space-y-2.5"
	>
		{#if errorMessage}
			<div
				role="alert"
				class="rounded-lg border border-status-danger/30 bg-status-danger-bg px-3 py-2 text-xs text-status-danger"
			>
				{errorMessage}
			</div>
		{/if}

		<div class="divide-y divide-accent-pop/15">
			{#each visibleRecommendations as rec (rec.categoryId)}
				{@const isApplying = applyingId === rec.categoryId}
				<div
					data-testid="recommendation-item"
					class="py-2 first:pt-0 last:pb-0 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5"
				>
					<div class="min-w-0 flex items-start gap-2.5">
						<span
							class="mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-accent-pop/15 text-accent-pop"
							aria-hidden="true"
						>
							<svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
								<path d="M3 17l6-6 4 4 8-8" />
								<path d="M17 7h4v4" />
							</svg>
						</span>

						<div class="min-w-0 space-y-1">
							<p class="text-xs leading-relaxed text-ink-primary">
								You averaged {fmtCurrency(rec.trailingMedianSpend)} on {rec.categoryName} over the last 3 months. Adjust target from {fmtCurrency(rec.currentPlanned)} to {fmtCurrency(rec.trailingMedianSpend)}?
							</p>
							<span
								class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-[11px] font-medium text-ink-secondary border border-border"
							>
								{deviationBadgeText(rec)}
							</span>
						</div>
					</div>

					<div class="flex items-center gap-2 shrink-0 sm:pl-2">
						<button
							type="button"
							disabled={applyingId !== null}
							onclick={() => void handleApply(rec)}
							class="rounded-lg bg-accent-primary text-accent-primary-text px-2.5 py-1.5 text-xs font-semibold hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							{isApplying ? 'Applying...' : 'Apply Recommended Target'}
						</button>
						<button
							type="button"
							aria-label={`Dismiss recommendation for ${rec.categoryName}`}
							onclick={() => handleDismiss(rec.categoryId)}
							class="rounded-lg px-2 py-1.5 text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
						>
							Dismiss
						</button>
					</div>
				</div>
			{/each}
		</div>

		{#if activeRecommendations.length > 2}
			<div class="pt-1 border-t border-accent-pop/15 flex justify-end">
				<button
					type="button"
					onclick={() => (showAll = !showAll)}
					class="text-xs font-medium text-accent-pop hover:underline"
				>
					{showAll ? 'Show fewer' : `Show ${hiddenCount} more`}
				</button>
			</div>
		{/if}
	</section>
{/if}
