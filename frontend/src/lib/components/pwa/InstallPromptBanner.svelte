<script lang="ts">
	import { pwaInstall } from '$lib/stores/pwaInstall.svelte';

	function handleInstallClick() {
		if (pwaInstall.installing) return;
		void pwaInstall.promptInstall();
	}
</script>

{#if pwaInstall.shouldShowBanner}
	<div
		data-testid="pwa-install-banner"
		class="flex flex-wrap items-center justify-between gap-2 px-3 sm:px-6 py-2 bg-accent-tint border-b border-accent-pop/25 text-xs shrink-0"
	>
		<div class="flex items-center gap-2.5 min-w-0">
			<span
				aria-hidden="true"
				class="w-6 h-6 rounded-md bg-accent-primary text-accent-primary-text flex items-center justify-center font-bold text-xs shrink-0"
			>
				$
			</span>
			<div class="min-w-0">
				<p class="font-semibold text-ink-primary">Install Budget App</p>
				<p class="text-[11px] text-ink-secondary">
					Add to your home screen for full-screen access and faster offline launch.
				</p>
				{#if pwaInstall.statusMessage}
					<p role="status" class="text-[11px] font-medium text-accent-pop mt-0.5">
						{pwaInstall.statusMessage}
					</p>
				{/if}
			</div>
		</div>

		<div class="flex items-center gap-1.5 shrink-0">
			<button
				type="button"
				data-testid="pwa-install-btn"
				aria-disabled={pwaInstall.installing ? 'true' : undefined}
				onclick={handleInstallClick}
				class="px-3 py-1.5 rounded-lg font-semibold bg-accent-primary text-accent-primary-text hover:opacity-90 aria-disabled:opacity-50 transition-opacity"
			>
				{pwaInstall.installing ? 'Installing...' : 'Install'}
			</button>
			<button
				type="button"
				data-testid="pwa-install-dismiss-btn"
				onclick={() => pwaInstall.dismiss()}
				class="px-2.5 py-1.5 rounded-lg font-medium text-ink-secondary hover:text-ink-primary hover:bg-surface-hover transition-colors"
			>
				Not now
			</button>
		</div>
	</div>
{/if}
