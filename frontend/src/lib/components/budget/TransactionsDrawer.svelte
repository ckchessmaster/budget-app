<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		isOpen: boolean;
		title?: string;
		badgeCount?: number;
		onClose: () => void;
		children?: Snippet;
	}

	let { isOpen, title = 'Transactions', badgeCount = 0, onClose, children }: Props = $props();

	$effect(() => {
		if (!isOpen) return;

		function handleKeyDown(e: KeyboardEvent) {
			if (e.key === 'Escape') {
				e.preventDefault();
				onClose();
			}
		}

		window.addEventListener('keydown', handleKeyDown);
		return () => {
			window.removeEventListener('keydown', handleKeyDown);
		};
	});
</script>

{#if isOpen}
	<div
		id="mobile-transactions-drawer"
		data-testid="mobile-transactions-drawer"
		role="dialog"
		aria-modal="true"
		aria-label={title}
		class="fixed inset-0 z-50 lg:hidden flex justify-end"
	>
		<button
			type="button"
			tabindex="-1"
			aria-label="Close transactions backdrop"
			data-testid="drawer-backdrop"
			onclick={onClose}
			class="fixed inset-0 bg-black/50 transition-opacity"
		></button>

		<div class="relative z-10 w-full max-w-md h-full bg-surface border-l border-border shadow-2xl flex flex-col">
			<div class="px-4 py-3 border-b border-border bg-subtle flex items-center justify-between shrink-0">
				<div class="flex items-center gap-2">
					<h2 class="text-sm font-bold text-ink-primary">{title}</h2>
					{#if badgeCount > 0}
						<span class="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full text-[11px] font-bold bg-accent-pop text-accent-primary-text">
							{badgeCount}
						</span>
					{/if}
				</div>

				<button
					type="button"
					aria-label="Close transactions drawer"
					data-testid="close-drawer-btn"
					onclick={onClose}
					class="p-1.5 rounded-lg text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
				>
					<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path d="M18 6 6 18" />
						<path d="m6 6 12 12" />
					</svg>
				</button>
			</div>

			<div class="flex-1 min-h-0 flex flex-col">
				{@render children?.()}
			</div>
		</div>
	</div>
{/if}
