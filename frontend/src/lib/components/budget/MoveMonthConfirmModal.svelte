<script lang="ts">
	interface Props {
		isOpen: boolean;
		merchantName: string;
		fromMonth: string; // YYYY-MM
		toMonth: string; // YYYY-MM
		targetLabel: string;
		isSubmitting?: boolean;
		onCancel: () => void;
		onConfirm: (dontRemindToday: boolean) => Promise<void> | void;
	}

	let {
		isOpen,
		merchantName,
		fromMonth,
		toMonth,
		targetLabel,
		isSubmitting = false,
		onCancel,
		onConfirm
	}: Props = $props();

	let dontRemindToday = $state(false);

	$effect(() => {
		if (isOpen) {
			dontRemindToday = false;
		}
	});

	function formatMonthLabel(ym: string): string {
		const [y, m] = ym.split('-').map(Number);
		if (!y || !m) return ym;
		return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString('en-US', {
			month: 'short',
			year: 'numeric',
			timeZone: 'UTC'
		});
	}

	function handleKeydown(e: KeyboardEvent) {
		if (isOpen && e.key === 'Escape' && !isSubmitting) {
			e.preventDefault();
			onCancel();
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

{#if isOpen}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<button
			type="button"
			tabindex="-1"
			aria-label="Close confirmation modal"
			onclick={() => {
				if (!isSubmitting) onCancel();
			}}
			class="fixed inset-0 bg-black/50 backdrop-blur-xs"
		></button>

		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="move-month-confirm-title"
			data-testid="move-month-confirm-modal"
			class="relative z-10 w-full max-w-md rounded-2xl border border-border bg-surface text-ink-primary p-6 shadow-2xl space-y-4"
		>
			<div class="flex items-start justify-between gap-3 border-b border-border pb-3">
				<h2 id="move-month-confirm-title" class="text-base font-semibold text-ink-primary">
					Move transaction to {formatMonthLabel(toMonth)}?
				</h2>
				<button
					type="button"
					aria-label="Close confirmation modal"
					disabled={isSubmitting}
					onclick={onCancel}
					class="rounded-lg p-1 text-ink-muted hover:bg-surface-hover hover:text-ink-primary disabled:opacity-40 transition-colors"
				>
					✕
				</button>
			</div>

			<p class="text-xs text-ink-secondary leading-relaxed">
				<strong class="text-ink-primary">{merchantName}</strong> is from
				<strong class="text-ink-primary">{formatMonthLabel(fromMonth)}</strong>. Adding it to
				<strong class="text-ink-primary">{targetLabel}</strong> here will move it into your
				<strong class="text-ink-primary">{formatMonthLabel(toMonth)}</strong> budget.
			</p>

			<label
				for="move-month-dont-remind"
				class="flex items-center gap-2 rounded-lg border border-border bg-subtle px-3 py-2 text-xs text-ink-primary cursor-pointer"
			>
				<input
					id="move-month-dont-remind"
					type="checkbox"
					bind:checked={dontRemindToday}
					class="rounded border-border bg-canvas accent-accent-pop"
				/>
				<span>Don't remind me again today</span>
			</label>

			<div class="flex items-center justify-end gap-2 pt-1">
				<button
					type="button"
					disabled={isSubmitting}
					onclick={onCancel}
					class="rounded-lg border border-border bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
				>
					Cancel
				</button>
				<button
					type="button"
					data-testid="move-month-confirm-btn"
					disabled={isSubmitting}
					onclick={() => void onConfirm(dontRemindToday)}
					class="rounded-lg bg-accent-primary px-4 py-2 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
				>
					{isSubmitting ? 'Moving...' : `Move to ${formatMonthLabel(toMonth)}`}
				</button>
			</div>
		</div>
	</div>
{/if}
