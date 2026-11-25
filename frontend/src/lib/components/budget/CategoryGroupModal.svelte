<script lang="ts">
	import { onMount } from 'svelte';
	import { GROUP_COLOR_PALETTE } from '$lib/utils/budgetColors';

	interface Props {
		isOpen: boolean;
		editingGroupName: string | null;
		initialColorIndex: number;
		existingGroupNames: string[];
		itemCount?: number;
		onClose: () => void;
		onSave: (payload: {
			oldName: string | null;
			name: string;
			colorIndex: number;
		}) => Promise<void> | void;
		onDelete?: (groupName: string) => Promise<void> | void;
	}

	let {
		isOpen,
		editingGroupName,
		initialColorIndex,
		existingGroupNames,
		itemCount = 0,
		onClose,
		onSave,
		onDelete
	}: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);
	let nameInput = $state('');
	let colorIndex = $state(0);
	let confirmDelete = $state(false);
	let isSubmitting = $state(false);
	let errorMsg = $state('');

	let isEditing = $derived(editingGroupName !== null);

	$effect(() => {
		if (!dialogEl) return;
		if (isOpen) {
			nameInput = editingGroupName ?? '';
			colorIndex = Math.max(0, initialColorIndex) % GROUP_COLOR_PALETTE.length;
			confirmDelete = false;
			errorMsg = '';
			if (!dialogEl.open) {
				dialogEl.showModal();
			}
		} else if (dialogEl.open) {
			dialogEl.close();
		}
	});

	// Fallback light-dismiss for browsers lacking native <dialog closedby="any">
	onMount(() => {
		if (!dialogEl || 'closedBy' in HTMLDialogElement.prototype) return;
		const el = dialogEl;
		let pressedOutside = false;
		const isOutside = (e: PointerEvent) => {
			const r = el.getBoundingClientRect();
			return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
		};
		const onDown = (e: PointerEvent) => {
			pressedOutside = el.open && !isSubmitting && isOutside(e);
		};
		const onUp = (e: PointerEvent) => {
			if (pressedOutside && el.open && !isSubmitting && isOutside(e)) {
				el.close();
			}
			pressedOutside = false;
		};
		el.addEventListener('pointerdown', onDown);
		el.addEventListener('pointerup', onUp);
		return () => {
			el.removeEventListener('pointerdown', onDown);
			el.removeEventListener('pointerup', onUp);
		};
	});

	function handleNativeClose() {
		if (dialogEl) {
			dialogEl.returnValue = '';
		}
		confirmDelete = false;
		errorMsg = '';
		onClose();
	}

	function handleCancel(e: Event) {
		if (isSubmitting) {
			e.preventDefault();
		}
	}

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		const trimmed = nameInput.trim();
		if (!trimmed) {
			errorMsg = 'Category name is required.';
			return;
		}
		if (trimmed.length > 100) {
			errorMsg = 'Category name must be 100 characters or fewer.';
			return;
		}
		const normalized = trimmed.toLowerCase();
		const oldNormalized = editingGroupName?.trim().toLowerCase() ?? null;
		const duplicate = existingGroupNames.some(
			(g) => g.trim().toLowerCase() === normalized && g.trim().toLowerCase() !== oldNormalized
		);
		if (duplicate) {
			errorMsg = `A category named "${trimmed}" already exists.`;
			return;
		}

		isSubmitting = true;
		errorMsg = '';
		try {
			await onSave({
				oldName: editingGroupName,
				name: trimmed,
				colorIndex
			});
			dialogEl?.close();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to save category.';
		} finally {
			isSubmitting = false;
		}
	}

	async function handleDeleteConfirmed() {
		if (!editingGroupName || !onDelete) return;
		isSubmitting = true;
		errorMsg = '';
		try {
			await onDelete(editingGroupName);
			dialogEl?.close();
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to delete category.';
		} finally {
			isSubmitting = false;
		}
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dialogEl}
	aria-labelledby="category-group-modal-title"
	closedby="any"
	onclose={handleNativeClose}
	oncancel={handleCancel}
	class="m-auto w-full max-w-md rounded-2xl border border-border bg-surface p-0 text-ink-primary shadow-2xl max-h-[90dvh] overflow-y-auto"
>
	<div class="p-6">
		<div class="flex items-center justify-between border-b border-border pb-3">
			<h2 id="category-group-modal-title" class="text-base font-semibold text-ink-primary">
				{isEditing ? 'Edit Category' : 'New Category'}
			</h2>
			<button
				type="button"
				aria-label="Close category modal"
				disabled={isSubmitting}
				onclick={() => dialogEl?.close()}
				class="rounded-lg p-1 text-ink-muted hover:bg-surface-hover hover:text-ink-primary disabled:opacity-40 transition-colors"
			>
				✕
			</button>
		</div>

		<form onsubmit={handleSubmit} class="mt-4 space-y-4">
			<div>
				<label for="group-modal-name" class="block text-xs font-medium text-ink-secondary mb-1">
					Category Name
				</label>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					id="group-modal-name"
					type="text"
					bind:value={nameInput}
					maxlength="100"
					placeholder="e.g. Essentials, Lifestyle, Savings"
					autofocus
					required
					class="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
				/>
			</div>

			<div>
				<span class="block text-xs font-medium text-ink-secondary mb-2">Color</span>
				<div class="flex flex-wrap items-center gap-2.5">
					{#each GROUP_COLOR_PALETTE as swatch, idx (swatch.name)}
						<button
							type="button"
							aria-label={swatch.name}
							aria-pressed={colorIndex === idx}
							title={swatch.name}
							onclick={() => (colorIndex = idx)}
							class="w-7 h-7 rounded-full {swatch.dotClass} transition-transform hover:scale-110 {colorIndex === idx
								? 'ring-2 ring-ink-primary ring-offset-2 ring-offset-surface scale-105'
								: ''}"
						></button>
					{/each}
				</div>
			</div>

			{#if errorMsg}
				<p
					role="alert"
					class="text-xs text-status-danger bg-status-danger-bg border border-status-danger/30 rounded-lg px-3 py-2"
				>
					{errorMsg}
				</p>
			{/if}

			{#if isEditing && confirmDelete && onDelete}
				<div class="rounded-lg border border-status-danger/40 bg-status-danger-bg p-3 space-y-2">
					<p class="text-xs text-status-danger">
						Delete "{editingGroupName}"{itemCount > 0
							? ` and its ${itemCount} ${itemCount === 1 ? 'item' : 'items'}`
							: ''}? This cannot be undone.
					</p>
					<div class="flex items-center justify-end gap-2">
						<button
							type="button"
							onclick={() => void handleDeleteConfirmed()}
							disabled={isSubmitting}
							class="rounded-lg bg-status-danger px-3 py-1.5 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
						>
							Confirm Delete
						</button>
						<button
							type="button"
							onclick={() => (confirmDelete = false)}
							disabled={isSubmitting}
							class="rounded-lg border border-border bg-subtle px-3 py-1.5 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
						>
							Cancel
						</button>
					</div>
				</div>
			{/if}

			<div class="flex items-center justify-between pt-2">
				{#if isEditing && !confirmDelete && onDelete}
					<button
						type="button"
						onclick={() => (confirmDelete = true)}
						disabled={isSubmitting}
						class="rounded-lg border border-status-danger/40 px-3 py-2 text-xs font-medium text-status-danger hover:bg-status-danger-bg disabled:opacity-50 transition-colors"
					>
						Delete
					</button>
				{:else}
					<div></div>
				{/if}

				<div class="flex items-center gap-2">
					<button
						type="button"
						onclick={() => dialogEl?.close()}
						disabled={isSubmitting}
						class="rounded-lg border border-border bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover hover:text-ink-primary transition-colors"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={isSubmitting}
						class="rounded-lg bg-accent-primary px-4 py-2 text-xs font-semibold text-accent-primary-text hover:opacity-90 disabled:opacity-50 transition-opacity"
					>
						{isSubmitting ? 'Saving...' : isEditing ? 'Save Category' : 'Create Category'}
					</button>
				</div>
			</div>
		</form>
	</div>
</dialog>

<style>
	dialog:modal {
		overscroll-behavior: contain;
	}
	dialog::backdrop {
		overflow: hidden;
		overscroll-behavior: contain;
		background: rgb(0 0 0 / 0.65);
		backdrop-filter: blur(4px);
	}
</style>
