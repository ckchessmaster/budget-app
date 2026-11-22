<script lang="ts">
	import { demo } from '$lib/stores/demo.svelte';
	import SimulatorWorkbench from './SimulatorWorkbench.svelte';

	let dialogEl = $state<HTMLDialogElement | null>(null);
	let confirmReset = $state(false);
	let resetting = $state(false);
	let pressedOutside = false;

	function isOutsideDialog(e: PointerEvent, dialog: HTMLDialogElement): boolean {
		const rect = dialog.getBoundingClientRect();
		return (
			e.clientX < rect.left ||
			e.clientX > rect.right ||
			e.clientY < rect.top ||
			e.clientY > rect.bottom
		);
	}

	$effect(() => {
		const el = dialogEl;
		if (!el) return;

		if (demo.drawerOpen) {
			if (!el.open) {
				if (typeof el.showModal === 'function') {
					el.showModal();
				} else {
					el.setAttribute('open', '');
				}
			}
		} else if (el.open) {
			if (typeof el.close === 'function') {
				el.close();
			} else {
				el.removeAttribute('open');
			}
		}
	});

	function handleDialogClose() {
		if (dialogEl) {
			dialogEl.returnValue = '';
		}
		confirmReset = false;
		if (demo.drawerOpen) {
			demo.closeDrawer();
		}
	}

	function handlePointerDown(e: PointerEvent) {
		if (!dialogEl || 'closedBy' in HTMLDialogElement.prototype) return;
		pressedOutside = isOutsideDialog(e, dialogEl);
	}

	function handlePointerUp(e: PointerEvent) {
		if (!dialogEl || 'closedBy' in HTMLDialogElement.prototype) return;
		if (pressedOutside && isOutsideDialog(e, dialogEl)) {
			requestClose();
		}
		pressedOutside = false;
	}

	function requestClose() {
		if (dialogEl && dialogEl.open && typeof dialogEl.close === 'function') {
			dialogEl.close();
		} else {
			demo.closeDrawer();
		}
	}

	async function handleResetDemo() {
		resetting = true;
		try {
			await demo.resetDemo();
			confirmReset = false;
		} finally {
			resetting = false;
		}
	}
</script>

<dialog
	bind:this={dialogEl}
	closedby="any"
	aria-labelledby="sim-drawer-title"
	onclose={handleDialogClose}
	onpointerdown={handlePointerDown}
	onpointerup={handlePointerUp}
	class="sim-drawer fixed inset-y-0 right-0 left-auto m-0 h-dvh max-h-dvh w-full max-w-xl bg-white dark:bg-slate-900 border-l border-slate-200 dark:border-slate-800 shadow-2xl p-0 text-slate-900 dark:text-slate-100"
>
	{#if demo.drawerOpen && demo.isDemo}
		<div class="flex flex-col h-full">
			<div class="flex items-center justify-between gap-3 px-5 py-4 border-b border-slate-200 dark:border-slate-800 shrink-0">
				<div>
					<h2 id="sim-drawer-title" class="text-base font-bold text-slate-900 dark:text-white">
						Fake SimpleFIN Simulator
					</h2>
					<p class="text-[11px] text-slate-500 dark:text-slate-400">
						Stage real-world bank events, then sync via Temporal while watching your ledger update.
					</p>
				</div>
				<!-- svelte-ignore a11y_autofocus -->
				<button
					type="button"
					autofocus
					onclick={requestClose}
					aria-label="Close simulator drawer"
					class="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-700 dark:hover:text-white transition-colors"
				>
					✕
				</button>
			</div>

			<div class="flex-1 overflow-y-auto overscroll-contain p-5">
				<SimulatorWorkbench compact={true} />
			</div>

			<div class="flex flex-wrap items-center justify-between gap-2 px-5 py-3 border-t border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950/60 shrink-0">
				<a
					href="/settings"
					onclick={requestClose}
					class="text-xs font-medium text-emerald-700 dark:text-emerald-400 hover:underline"
				>
					Open Full Settings →
				</a>

				<div class="flex items-center gap-2">
					{#if confirmReset}
						<button
							type="button"
							disabled={resetting}
							onclick={handleResetDemo}
							class="px-2.5 py-1 rounded text-xs font-semibold bg-rose-600 hover:bg-rose-500 text-white"
						>
							{resetting ? 'Resetting...' : 'Confirm Reset'}
						</button>
						<button
							type="button"
							onclick={() => (confirmReset = false)}
							class="px-2.5 py-1 rounded text-xs text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-800"
						>
							Cancel
						</button>
					{:else}
						<button
							type="button"
							onclick={() => (confirmReset = true)}
							class="px-2.5 py-1 rounded text-xs font-medium text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10"
						>
							Reset Demo Data
						</button>
					{/if}
				</div>
			</div>
		</div>
	{/if}
</dialog>

<style>
	.sim-drawer:not([open]) {
		display: none;
	}
	.sim-drawer[open] {
		display: flex;
		flex-direction: column;
	}
	.sim-drawer:modal {
		overscroll-behavior: contain;
	}
	.sim-drawer::backdrop {
		background: rgb(0 0 0 / 0.5);
		backdrop-filter: blur(2px);
		overflow: hidden;
		overscroll-behavior: contain;
	}
</style>
