<script lang="ts">
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import { workspace } from '$lib/stores/workspace.svelte';

	let open = $state(false);
	let creating = $state(false);
	let newName = $state('');
	let submitting = $state(false);
	let errorMsg = $state<string | null>(null);
	let rootEl = $state<HTMLDivElement | null>(null);

	function roleLabel(role: string): string {
		if (role === 'read_only') return 'Read-Only';
		if (role === 'member') return 'Member';
		return 'Owner';
	}

	function roleBadgeClass(role: string): string {
		if (role === 'read_only') {
			return 'bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300';
		}
		if (role === 'member') {
			return 'bg-sky-100 text-sky-800 dark:bg-sky-500/20 dark:text-sky-300';
		}
		return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-300';
	}

	async function handleSelect(id: string) {
		open = false;
		creating = false;
		errorMsg = null;
		await workspace.switchWorkspace(id);
	}

	async function handleCreatePersonal(e: SubmitEvent) {
		e.preventDefault();
		const trimmed = newName.trim();
		if (!trimmed) return;
		submitting = true;
		errorMsg = null;
		try {
			await workspace.createPersonal(trimmed);
			newName = '';
			creating = false;
			open = false;
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to create personal workspace';
		} finally {
			submitting = false;
		}
	}

	function handleWindowClick(e: MouseEvent) {
		if (open && rootEl && !rootEl.contains(e.target as Node)) {
			open = false;
		}
	}
</script>

<svelte:window onclick={handleWindowClick} />

{#if auth.isAuthenticated && workspace.workspaces.length > 0}
	<div bind:this={rootEl} class="relative shrink-0" data-testid="workspace-switcher">
		<button
			type="button"
			data-testid="workspace-switcher-btn"
			aria-haspopup="listbox"
			aria-expanded={open}
			onclick={() => {
				open = !open;
				errorMsg = null;
			}}
			class="inline-flex items-center gap-1.5 rounded-lg border border-border bg-subtle hover:bg-surface-hover px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors max-w-[180px] sm:max-w-[230px]"
		>
			<span class="truncate font-semibold">
				{demo.isDemo ? 'Demo Sandbox' : workspace.activeWorkspace?.name ?? 'Workspace'}
			</span>
			{#if demo.isDemo}
				<span class="shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-bold bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">
					Demo
				</span>
			{:else if workspace.currentRole !== 'owner'}
				<span
					data-testid="workspace-role-pill"
					class="shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-bold {roleBadgeClass(workspace.currentRole)}"
				>
					{roleLabel(workspace.currentRole)}
				</span>
			{/if}
			<svg class="w-3.5 h-3.5 shrink-0 text-ink-secondary" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
				<path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
			</svg>
		</button>

		{#if open}
			<div
				role="listbox"
				aria-label="Switch workspace"
				data-testid="workspace-switcher-menu"
				class="absolute left-0 mt-1.5 w-64 rounded-xl border border-border bg-surface shadow-xl z-50 p-1.5 space-y-1"
			>
				{#if demo.isDemo}
					<div class="px-2.5 py-2 text-[11px] text-amber-800 dark:text-amber-200 bg-amber-50 dark:bg-amber-500/10 rounded-lg">
						Demo Mode is active. Exit Demo Mode in Settings or the top banner to switch real workspaces.
					</div>
				{:else}
					<div class="px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
						Workspaces
					</div>
					{#each workspace.workspaces.filter((w) => !w.is_demo) as ws (ws.id)}
						{@const isActive = ws.id === workspace.activeWorkspaceId}
						<button
							type="button"
							role="option"
							aria-selected={isActive}
							data-testid="workspace-option-{ws.id}"
							onclick={() => void handleSelect(ws.id)}
							class="w-full flex items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-xs transition-colors {isActive
								? 'bg-accent-tint text-accent-pop font-semibold'
								: 'text-ink-primary hover:bg-surface-hover'}"
						>
							<span class="truncate">{ws.name}</span>
							<span class="shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-semibold {roleBadgeClass(ws.role)}">
								{roleLabel(ws.role)}
							</span>
						</button>
					{/each}

					{#if workspace.canCreatePersonalWorkspace}
						<div class="pt-1 border-t border-border">
							{#if creating}
								<form onsubmit={handleCreatePersonal} class="p-1.5 space-y-2">
									<input
										type="text"
										bind:value={newName}
										placeholder="My Personal Budget"
										maxlength="255"
										required
										class="w-full rounded-md border border-border bg-subtle px-2.5 py-1.5 text-xs text-ink-primary"
									/>
									{#if errorMsg}
										<p class="text-[11px] text-status-danger">{errorMsg}</p>
									{/if}
									<div class="flex items-center justify-end gap-1.5">
										<button
											type="button"
											onclick={() => {
												creating = false;
												errorMsg = null;
											}}
											class="px-2 py-1 text-[11px] text-ink-secondary hover:text-ink-primary"
										>
											Cancel
										</button>
										<button
											type="submit"
											disabled={submitting || !newName.trim()}
											class="px-2.5 py-1 rounded-md text-[11px] font-semibold bg-accent-primary text-accent-primary-text disabled:opacity-50"
										>
											{submitting ? 'Creating...' : 'Create'}
										</button>
									</div>
								</form>
							{:else}
								<button
									type="button"
									data-testid="create-personal-workspace-btn"
									onclick={() => {
										creating = true;
										newName = 'Personal Workspace';
									}}
									class="w-full text-left rounded-lg px-2.5 py-2 text-xs font-medium text-accent-pop hover:bg-surface-hover transition-colors"
								>
									+ Create Personal Workspace
								</button>
							{/if}
						</div>
					{/if}
				{/if}
			</div>
		{/if}
	</div>
{/if}
