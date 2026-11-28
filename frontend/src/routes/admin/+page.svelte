<script lang="ts">
	import { onMount } from 'svelte';
	import {
		createPlatformInvitation,
		deletePlatformUser,
		fetchAdminHealth,
		fetchAdminWorkspaces,
		fetchPlatformInvitations,
		fetchPlatformUsers,
		revokePlatformInvitation,
		updatePlatformUserStatus,
		type AdminHealthResponse,
		type AdminWorkspaceOverview,
		type PlatformInvitation,
		type PlatformUser
	} from '$lib/api/admin';
	import { ApiError } from '$lib/api/workspaces';
	import { auth } from '$lib/stores/auth.svelte';

	type AdminTab = 'invites' | 'users' | 'health';
	const TABS: Array<{ id: AdminTab; label: string }> = [
		{ id: 'invites', label: 'Invites' },
		{ id: 'users', label: 'Users & Workspaces' },
		{ id: 'health', label: 'System Health' }
	];

	let activeTab = $state<AdminTab>('invites');
	let loading = $state(true);
	let errorBanner = $state<string | null>(null);

	let invitations = $state<PlatformInvitation[]>([]);
	let users = $state<PlatformUser[]>([]);
	let workspaces = $state<AdminWorkspaceOverview[]>([]);
	let health = $state<AdminHealthResponse | null>(null);

	// Generate invite form state
	let inviteLabel = $state('');
	let inviteExpiresHours = $state(168);
	let inviteEmail = $state('');
	let creatingInvite = $state(false);
	let revokingInviteId = $state<string | null>(null);

	// Copy Link modal state
	let copyDialogEl: HTMLDialogElement | null = $state(null);
	let createdInvite = $state<PlatformInvitation | null>(null);
	let copied = $state(false);

	// User lifecycle state
	let busyUserId = $state<string | null>(null);
	let deleteDialogEl: HTMLDialogElement | null = $state(null);
	let userToDelete = $state<PlatformUser | null>(null);
	let confirmEmailInput = $state('');
	let deletingUser = $state(false);

	let healthLoading = $state(false);

	function errText(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Request failed';
	}

	async function loadAll() {
		loading = true;
		errorBanner = null;
		try {
			const [invList, userList, wsList, healthRes] = await Promise.all([
				fetchPlatformInvitations(),
				fetchPlatformUsers(),
				fetchAdminWorkspaces(),
				fetchAdminHealth()
			]);
			invitations = invList;
			users = userList;
			workspaces = wsList;
			health = healthRes;
		} catch (err) {
			errorBanner = errText(err);
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void loadAll();
	});

	async function refreshHealth() {
		healthLoading = true;
		errorBanner = null;
		try {
			health = await fetchAdminHealth();
		} catch (err) {
			errorBanner = errText(err);
		} finally {
			healthLoading = false;
		}
	}

	function handleTabKeydown(e: KeyboardEvent, idx: number) {
		let nextIdx = idx;
		if (e.key === 'ArrowRight') nextIdx = (idx + 1) % TABS.length;
		else if (e.key === 'ArrowLeft') nextIdx = (idx - 1 + TABS.length) % TABS.length;
		else if (e.key === 'Home') nextIdx = 0;
		else if (e.key === 'End') nextIdx = TABS.length - 1;
		else return;

		e.preventDefault();
		const target = TABS[nextIdx];
		activeTab = target.id;
		const btn = document.getElementById(`admin-tab-${target.id}`);
		btn?.focus();
	}

	async function handleCreateInvite(e: SubmitEvent) {
		e.preventDefault();
		creatingInvite = true;
		errorBanner = null;
		try {
			const created = await createPlatformInvitation({
				label: inviteLabel.trim(),
				expires_in_hours: Number(inviteExpiresHours) || 168,
				email: inviteEmail.trim() || undefined
			});
			invitations = [created, ...invitations];
			inviteLabel = '';
			inviteEmail = '';
			createdInvite = created;
			copied = false;
			copyDialogEl?.showModal();
		} catch (err) {
			errorBanner = errText(err);
		} finally {
			creatingInvite = false;
		}
	}

	function closeCopyDialog() {
		copyDialogEl?.close();
		createdInvite = null;
		copied = false;
	}

	async function copyInviteURL() {
		if (!createdInvite?.invite_url) return;
		try {
			await navigator.clipboard?.writeText(createdInvite.invite_url);
			copied = true;
		} catch {
			copied = false;
		}
	}

	async function handleRevokeInvite(id: string) {
		revokingInviteId = id;
		errorBanner = null;
		try {
			await revokePlatformInvitation(id);
			invitations = invitations.map((inv) =>
				inv.id === id ? { ...inv, status: 'REVOKED' } : inv
			);
		} catch (err) {
			errorBanner = errText(err);
		} finally {
			revokingInviteId = null;
		}
	}

	async function handleToggleUserStatus(user: PlatformUser) {
		busyUserId = user.user_id;
		errorBanner = null;
		try {
			const updated = await updatePlatformUserStatus(user.user_id, !user.is_active);
			users = users.map((u) =>
				u.user_id === user.user_id ? { ...u, is_active: updated.is_active } : u
			);
		} catch (err) {
			errorBanner = errText(err);
		} finally {
			busyUserId = null;
		}
	}

	function openDeleteModal(user: PlatformUser) {
		userToDelete = user;
		confirmEmailInput = '';
		deleteDialogEl?.showModal();
	}

	function closeDeleteModal() {
		deleteDialogEl?.close();
		userToDelete = null;
		confirmEmailInput = '';
	}

	async function confirmDeleteUser(e: SubmitEvent) {
		e.preventDefault();
		if (!userToDelete) return;
		if (confirmEmailInput.trim().toLowerCase() !== userToDelete.email.toLowerCase()) return;

		deletingUser = true;
		errorBanner = null;
		try {
			await deletePlatformUser(userToDelete.user_id);
			const deletedId = userToDelete.user_id;
			const deletedEmail = userToDelete.email.toLowerCase();
			users = users.filter((u) => u.user_id !== deletedId);
			workspaces = workspaces.filter((w) => w.owner_email.toLowerCase() !== deletedEmail);
			closeDeleteModal();
		} catch (err) {
			errorBanner = errText(err);
		} finally {
			deletingUser = false;
		}
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'ACTIVE':
			case 'OK':
			case 'ok':
				return 'bg-status-good-bg text-status-good';
			case 'REDEEMED':
				return 'bg-accent-tint text-accent-pop';
			case 'ERROR':
			case 'REVOKED':
			case 'degraded':
			case 'error':
				return 'bg-status-danger-bg text-status-danger';
			default:
				return 'bg-status-warn-bg text-status-warn';
		}
	}
</script>

<div class="max-w-6xl mx-auto px-4 sm:px-6 py-6 space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h2 class="text-xl font-bold text-ink-primary">Platform Administration</h2>
			<p class="text-xs text-ink-secondary mt-0.5">
				Invite-only friend onboarding, user lifecycle, privacy-safe workspace metadata, and system health.
			</p>
		</div>
	</div>

	{#if errorBanner}
		<div
			data-testid="admin-error-banner"
			role="alert"
			class="rounded-xl border border-status-danger/30 bg-status-danger-bg px-4 py-3 text-xs text-status-danger font-medium"
		>
			Error: {errorBanner}
		</div>
	{/if}

	<div class="border-b border-border overflow-x-auto">
		<div
			role="tablist"
			aria-label="Platform Administration Sections"
			class="flex items-center gap-1 min-w-max"
		>
			{#each TABS as tab, idx}
				<button
					type="button"
					role="tab"
					id="admin-tab-{tab.id}"
					aria-selected={activeTab === tab.id}
					tabindex={activeTab === tab.id ? 0 : -1}
					onclick={() => (activeTab = tab.id)}
					onkeydown={(e) => handleTabKeydown(e, idx)}
					data-testid="admin-tab-{tab.id}"
					class="admin-tab relative px-4 py-2.5 text-xs font-semibold transition-colors whitespace-nowrap {activeTab === tab.id
						? 'text-accent-pop'
						: 'text-ink-secondary hover:text-ink-primary'}"
				>
					{tab.label}
				</button>
			{/each}
		</div>
	</div>

	{#if loading}
		<div class="py-10 text-center text-xs text-ink-secondary">Loading platform admin data...</div>
	{:else}
		<!-- Tab 1: Invites -->
		<div role="tabpanel" aria-labelledby="admin-tab-invites" hidden={activeTab !== 'invites'}>
			<div class="space-y-6">
				<section class="rounded-xl border border-border bg-surface p-5 space-y-4">
					<div>
						<h3 class="text-sm font-semibold text-ink-primary">Generate Single-Use Invite Link</h3>
						<p class="text-xs text-ink-secondary mt-0.5">
							Creates a single-use token for a friend to register their Authentik account. Optionally send it via email.
						</p>
					</div>

					<form onsubmit={handleCreateInvite} class="grid grid-cols-1 sm:grid-cols-4 gap-3 items-end">
						<div>
							<label for="invite-label" class="block text-xs font-medium text-ink-primary mb-1">
								Label (optional)
							</label>
							<input
								id="invite-label"
								type="text"
								maxlength="120"
								placeholder="For Mike"
								bind:value={inviteLabel}
								class="w-full rounded-lg border border-border bg-subtle px-3 py-2 text-xs text-ink-primary"
							/>
						</div>

						<div>
							<label for="invite-expiration" class="block text-xs font-medium text-ink-primary mb-1">
								Expires In
							</label>
							<select
								id="invite-expiration"
								bind:value={inviteExpiresHours}
								class="w-full rounded-lg border border-border bg-subtle px-3 py-2 text-xs text-ink-primary"
							>
								<option value={24}>24 hours</option>
								<option value={168}>7 days</option>
								<option value={720}>30 days</option>
							</select>
						</div>

						<div>
							<label for="invite-email" class="block text-xs font-medium text-ink-primary mb-1">
								Send via Email (optional)
							</label>
							<input
								id="invite-email"
								type="email"
								spellcheck="false"
								placeholder="friend@example.com"
								bind:value={inviteEmail}
								class="w-full rounded-lg border border-border bg-subtle px-3 py-2 text-xs text-ink-primary"
							/>
						</div>

						<button
							type="submit"
							disabled={creatingInvite}
							data-testid="generate-invite-btn"
							class="rounded-lg bg-accent-primary text-accent-primary-text px-4 py-2 text-xs font-semibold hover:opacity-90 disabled:opacity-50"
						>
							{creatingInvite ? 'Generating...' : 'Generate Invite Link'}
						</button>
					</form>
				</section>

				<section class="rounded-xl border border-border bg-surface overflow-hidden">
					<div class="px-5 py-3.5 border-b border-border flex items-center justify-between">
						<h3 class="text-sm font-semibold text-ink-primary">Platform Invitations</h3>
						<span class="text-xs text-ink-secondary">{invitations.length} total</span>
					</div>

					<div class="overflow-x-auto">
						<table class="w-full text-left text-xs">
							<thead class="bg-subtle text-ink-secondary border-b border-border">
								<tr>
									<th class="px-4 py-2.5 font-medium">Label</th>
									<th class="px-4 py-2.5 font-medium">Recipient</th>
									<th class="px-4 py-2.5 font-medium">Status</th>
									<th class="px-4 py-2.5 font-medium">Expires</th>
									<th class="px-4 py-2.5 font-medium">Redeemed</th>
									<th class="px-4 py-2.5 font-medium text-right">Actions</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#if invitations.length === 0}
									<tr>
										<td colspan="6" class="px-4 py-6 text-center text-ink-secondary">
											No platform invitations generated yet.
										</td>
									</tr>
								{:else}
									{#each invitations as inv (inv.id)}
										<tr data-testid="invite-row-{inv.id}">
											<td class="px-4 py-3 font-medium text-ink-primary">
												{inv.label || 'Untitled invite'}
											</td>
											<td class="px-4 py-3 text-ink-secondary font-mono">
												{inv.recipient_email || '—'}
											</td>
											<td class="px-4 py-3">
												<span class="inline-flex px-2 py-0.5 rounded-full text-[11px] font-semibold {statusBadgeClass(inv.status)}">
													{inv.status}
												</span>
											</td>
											<td class="px-4 py-3 text-ink-secondary">
												{new Date(inv.expires_at).toLocaleDateString()}
											</td>
											<td class="px-4 py-3 text-ink-secondary">
												{#if inv.redeemed_at}
													<span>{inv.redeemed_by_email || 'Redeemed'} · {new Date(inv.redeemed_at).toLocaleDateString()}</span>
												{:else}
													—
												{/if}
											</td>
											<td class="px-4 py-3 text-right">
												{#if inv.status === 'ACTIVE'}
													<button
														type="button"
														disabled={revokingInviteId === inv.id}
														onclick={() => void handleRevokeInvite(inv.id)}
														data-testid="revoke-invite-{inv.id}"
														class="rounded-md border border-status-danger/40 px-2.5 py-1 text-[11px] font-semibold text-status-danger hover:bg-status-danger-bg disabled:opacity-50"
													>
														{revokingInviteId === inv.id ? 'Revoking...' : 'Revoke'}
													</button>
												{:else}
													<span class="text-[11px] text-ink-secondary">—</span>
												{/if}
											</td>
										</tr>
									{/each}
								{/if}
							</tbody>
						</table>
					</div>
				</section>
			</div>
		</div>

		<!-- Tab 2: Users & Workspaces -->
		<div role="tabpanel" aria-labelledby="admin-tab-users" hidden={activeTab !== 'users'}>
			<div class="space-y-6">
				<section class="rounded-xl border border-border bg-surface overflow-hidden">
					<div class="px-5 py-3.5 border-b border-border flex items-center justify-between">
						<div>
							<h3 class="text-sm font-semibold text-ink-primary">Platform Users</h3>
							<p class="text-[11px] text-ink-secondary">
								Account lifecycle synchronized with Authentik. Disabling revokes active sessions immediately.
							</p>
						</div>
						<span class="text-xs text-ink-secondary">{users.length} users</span>
					</div>

					<div class="overflow-x-auto">
						<table class="w-full text-left text-xs">
							<thead class="bg-subtle text-ink-secondary border-b border-border">
								<tr>
									<th class="px-4 py-2.5 font-medium">User</th>
									<th class="px-4 py-2.5 font-medium">Status</th>
									<th class="px-4 py-2.5 font-medium">Workspaces</th>
									<th class="px-4 py-2.5 font-medium">Last Login</th>
									<th class="px-4 py-2.5 font-medium text-right">Actions</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#if users.length === 0}
									<tr>
										<td colspan="5" class="px-4 py-6 text-center text-ink-secondary">
											No platform users registered yet.
										</td>
									</tr>
								{:else}
									{#each users as user (user.user_id)}
										{@const isSelf = auth.user?.id === user.user_id}
										<tr data-testid="user-row-{user.user_id}">
											<td class="px-4 py-3">
												<div class="font-medium text-ink-primary">{user.display_name || user.email}</div>
												<div class="text-[11px] text-ink-secondary font-mono">{user.email}</div>
											</td>
											<td class="px-4 py-3">
												<span class="inline-flex px-2 py-0.5 rounded-full text-[11px] font-semibold {user.is_active ? 'bg-status-good-bg text-status-good' : 'bg-status-danger-bg text-status-danger'}">
													{user.is_active ? 'Active' : 'Disabled'}
												</span>
											</td>
											<td class="px-4 py-3 text-ink-secondary">
												{user.owned_workspace_count} owned · {user.member_workspace_count} shared
											</td>
											<td class="px-4 py-3 text-ink-secondary">
												{user.last_login_at ? new Date(user.last_login_at).toLocaleString() : 'Never'}
											</td>
											<td class="px-4 py-3 text-right space-x-2 whitespace-nowrap">
												<button
													type="button"
													disabled={isSelf || busyUserId === user.user_id}
													onclick={() => void handleToggleUserStatus(user)}
													data-testid="toggle-user-{user.user_id}"
													class="rounded-md border border-border bg-subtle px-2.5 py-1 text-[11px] font-semibold text-ink-primary hover:bg-surface-hover disabled:opacity-40"
												>
													{user.is_active ? 'Disable' : 'Enable'}
												</button>
												<button
													type="button"
													disabled={isSelf || busyUserId === user.user_id}
													onclick={() => openDeleteModal(user)}
													data-testid="delete-user-{user.user_id}"
													class="rounded-md border border-status-danger/40 px-2.5 py-1 text-[11px] font-semibold text-status-danger hover:bg-status-danger-bg disabled:opacity-40"
												>
													Delete
												</button>
											</td>
										</tr>
									{/each}
								{/if}
							</tbody>
						</table>
					</div>
				</section>

				<section class="rounded-xl border border-border bg-surface overflow-hidden">
					<div class="px-5 py-3.5 border-b border-border flex items-center justify-between">
						<div>
							<h3 class="text-sm font-semibold text-ink-primary">Workspaces (Metadata Only)</h3>
							<p class="text-[11px] text-ink-secondary">
								Privacy-enforced view: zero visibility into accounts, balances, categories, or transactions.
							</p>
						</div>
						<span class="text-xs text-ink-secondary">{workspaces.length} workspaces</span>
					</div>

					<div class="overflow-x-auto">
						<table class="w-full text-left text-xs">
							<thead class="bg-subtle text-ink-secondary border-b border-border">
								<tr>
									<th class="px-4 py-2.5 font-medium">Workspace</th>
									<th class="px-4 py-2.5 font-medium">Owner</th>
									<th class="px-4 py-2.5 font-medium">Members</th>
									<th class="px-4 py-2.5 font-medium">SimpleFIN Status</th>
									<th class="px-4 py-2.5 font-medium">Last Synced</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#if workspaces.length === 0}
									<tr>
										<td colspan="5" class="px-4 py-6 text-center text-ink-secondary">
											No workspaces found.
										</td>
									</tr>
								{:else}
									{#each workspaces as ws (ws.workspace_id)}
										<tr>
											<td class="px-4 py-3 font-medium text-ink-primary">{ws.workspace_name}</td>
											<td class="px-4 py-3 font-mono text-ink-secondary">{ws.owner_email || '—'}</td>
											<td class="px-4 py-3 text-ink-secondary">{ws.member_count}</td>
											<td class="px-4 py-3">
												<span class="inline-flex px-2 py-0.5 rounded-full text-[11px] font-semibold {statusBadgeClass(ws.simplefin_status)}">
													{ws.simplefin_status}
												</span>
												{#if ws.simplefin_error}
													<div class="text-[11px] text-status-danger mt-1">{ws.simplefin_error}</div>
												{/if}
											</td>
											<td class="px-4 py-3 text-ink-secondary">
												{ws.simplefin_last_synced_at ? new Date(ws.simplefin_last_synced_at).toLocaleString() : '—'}
											</td>
										</tr>
									{/each}
								{/if}
							</tbody>
						</table>
					</div>
				</section>
			</div>
		</div>

		<!-- Tab 3: System Health -->
		<div role="tabpanel" aria-labelledby="admin-tab-health" hidden={activeTab !== 'health'}>
			<div class="space-y-6">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-2.5">
						<span class="text-sm font-semibold text-ink-primary">Overall Status</span>
						{#if health}
							<span
								data-testid="health-overall-status"
								class="inline-flex px-2.5 py-0.5 rounded-full text-xs font-semibold uppercase {statusBadgeClass(health.status)}"
							>
								{health.status}
							</span>
						{/if}
					</div>
					<button
						type="button"
						disabled={healthLoading}
						onclick={() => void refreshHealth()}
						class="rounded-lg border border-border bg-surface px-3 py-1.5 text-xs font-semibold text-ink-primary hover:bg-surface-hover disabled:opacity-50"
					>
						{healthLoading ? 'Refreshing...' : 'Refresh'}
					</button>
				</div>

				{#if health}
					<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
						<div class="rounded-xl border border-border bg-surface p-4 space-y-1.5">
							<div class="text-xs text-ink-secondary font-medium">PostgreSQL</div>
							<div class="flex items-center justify-between">
								<span class="inline-flex px-2 py-0.5 rounded-full text-[11px] font-semibold {statusBadgeClass(health.dependencies.postgres?.status || 'error')}">
									{health.dependencies.postgres?.status || 'unknown'}
								</span>
								<span class="text-xs font-mono text-ink-secondary">
									{health.dependencies.postgres?.latency_ms ?? 0} ms
								</span>
							</div>
						</div>

						<div class="rounded-xl border border-border bg-surface p-4 space-y-1.5">
							<div class="text-xs text-ink-secondary font-medium">Temporal</div>
							<div class="flex items-center justify-between">
								<span class="inline-flex px-2 py-0.5 rounded-full text-[11px] font-semibold {statusBadgeClass(health.dependencies.temporal?.status || 'error')}">
									{health.dependencies.temporal?.status || 'unknown'}
								</span>
								<span class="text-xs font-mono text-ink-secondary">
									{health.dependencies.temporal?.latency_ms ?? 0} ms
								</span>
							</div>
						</div>

						<div class="rounded-xl border border-border bg-surface p-4 space-y-1.5">
							<div class="text-xs text-ink-secondary font-medium">24h Workflow Failures</div>
							<div class="text-lg font-bold font-mono text-ink-primary">
								{health.temporal_workflows_24h.failed_bank_syncs +
									health.temporal_workflows_24h.failed_receipt_workflows +
									(health.temporal_workflows_24h.failed_invite_emails ?? 0)}
							</div>
							<div class="text-[11px] text-ink-secondary">
								Sync: {health.temporal_workflows_24h.failed_bank_syncs} · Invite: {health.temporal_workflows_24h.failed_invite_emails ?? 0}
							</div>
						</div>

						<div class="rounded-xl border border-border bg-surface p-4 space-y-1.5">
							<div class="text-xs text-ink-secondary font-medium">24h Resend Delivery</div>
							<div class="text-lg font-bold font-mono text-ink-primary">
								{health.outbound_email_24h.sent} sent / {health.outbound_email_24h.failed} failed
							</div>
							<div class="text-[11px] text-ink-secondary">
								{health.simplefin.configured_workspaces} configured bank connections
							</div>
						</div>
					</div>

					<section class="rounded-xl border border-border bg-surface overflow-hidden">
						<div class="px-5 py-3.5 border-b border-border">
							<h3 class="text-sm font-semibold text-ink-primary">Stale or Errored SimpleFIN Syncs</h3>
						</div>
						<div class="overflow-x-auto">
							<table class="w-full text-left text-xs">
								<thead class="bg-subtle text-ink-secondary border-b border-border">
									<tr>
										<th class="px-4 py-2.5 font-medium">Workspace</th>
										<th class="px-4 py-2.5 font-medium">Status</th>
										<th class="px-4 py-2.5 font-medium">Last Synced</th>
										<th class="px-4 py-2.5 font-medium">Error Summary</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-border">
									{#if health.simplefin.stale_or_errored.length === 0}
										<tr>
											<td colspan="4" class="px-4 py-6 text-center text-ink-secondary">
												All configured SimpleFIN bank connections are healthy.
											</td>
										</tr>
									{:else}
										{#each health.simplefin.stale_or_errored as item (item.workspace_id)}
											<tr>
												<td class="px-4 py-3 font-medium text-ink-primary">{item.workspace_name}</td>
												<td class="px-4 py-3">
													<span class="inline-flex px-2 py-0.5 rounded-full text-[11px] font-semibold {statusBadgeClass(item.status)}">
														{item.status}
													</span>
												</td>
												<td class="px-4 py-3 text-ink-secondary">
													{item.last_synced_at ? new Date(item.last_synced_at).toLocaleString() : 'Never'}
												</td>
												<td class="px-4 py-3 text-status-danger">
													{item.error_summary || 'Last sync older than 12 hours'}
												</td>
											</tr>
										{/each}
									{/if}
								</tbody>
							</table>
						</div>
					</section>
				{/if}
			</div>
		</div>
	{/if}
</div>

<!-- Native Copy Link Modal -->
<dialog
	bind:this={copyDialogEl}
	aria-labelledby="copy-invite-dialog-title"
	onclose={() => {
		createdInvite = null;
		copied = false;
	}}
	class="rounded-2xl border border-border bg-surface text-ink-primary p-6 shadow-2xl w-full max-w-md backdrop:bg-black/50"
>
	<h1 id="copy-invite-dialog-title" class="text-base font-bold text-ink-primary">
		Single-Use Invite Link Created
	</h1>
	<p class="text-xs text-ink-secondary mt-1">
		Copy and share this link now. For security, the raw token cannot be viewed again after closing this dialog.
	</p>

	{#if createdInvite?.email_dispatched}
		<p class="mt-3 rounded-lg bg-status-good-bg text-status-good px-3 py-2 text-xs font-medium">
			An invitation email has also been queued for {createdInvite.recipient_email}.
		</p>
	{/if}

	<div class="mt-4 rounded-lg border border-border bg-subtle p-3">
		<code data-testid="generated-invite-url" class="block text-[11px] break-all text-ink-primary">
			{createdInvite?.invite_url || ''}
		</code>
	</div>

	<div class="mt-5 flex items-center justify-end gap-2">
		<button
			type="button"
			onclick={closeCopyDialog}
			class="rounded-lg border border-border px-3 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover"
		>
			Close
		</button>
		<!-- svelte-ignore a11y_autofocus -->
		<button
			type="button"
			autofocus
			onclick={() => void copyInviteURL()}
			data-testid="copy-invite-url-btn"
			class="rounded-lg bg-accent-primary text-accent-primary-text px-4 py-2 text-xs font-semibold hover:opacity-90"
		>
			{copied ? 'Copied!' : 'Copy Link'}
		</button>
	</div>
</dialog>

<!-- Native Delete User Confirmation Modal -->
<dialog
	bind:this={deleteDialogEl}
	aria-labelledby="delete-user-dialog-title"
	onclose={() => {
		userToDelete = null;
		confirmEmailInput = '';
	}}
	class="rounded-2xl border border-status-danger/40 bg-surface text-ink-primary p-6 shadow-2xl w-full max-w-md backdrop:bg-black/50"
>
	<h1 id="delete-user-dialog-title" class="text-base font-bold text-status-danger">
		Permanently Delete User
	</h1>
	<p class="text-xs text-ink-secondary mt-2 leading-relaxed">
		Deleting <strong class="text-ink-primary">{userToDelete?.email}</strong> will remove their Authentik account,
		revoke access, and <strong class="text-ink-primary">permanently cascade-delete any personal workspace they solely own</strong>
		along with all its financial records.
	</p>

	<form onsubmit={confirmDeleteUser} class="mt-4 space-y-4">
		<div>
			<label for="confirm-delete-email" class="block text-xs font-medium text-ink-primary mb-1">
				Type <span class="font-mono">{userToDelete?.email}</span> to confirm
			</label>
			<input
				id="confirm-delete-email"
				type="email"
				spellcheck="false"
				autocomplete="off"
				bind:value={confirmEmailInput}
				data-testid="confirm-delete-email-input"
				class="w-full rounded-lg border border-border bg-subtle px-3 py-2 text-xs text-ink-primary"
			/>
		</div>

		<div class="flex items-center justify-end gap-2">
			<!-- svelte-ignore a11y_autofocus -->
			<button
				type="button"
				autofocus
				onclick={closeDeleteModal}
				class="rounded-lg border border-border px-3 py-2 text-xs font-medium text-ink-secondary hover:bg-surface-hover"
			>
				Cancel
			</button>
			<button
				type="submit"
				disabled={deletingUser || !userToDelete || confirmEmailInput.trim().toLowerCase() !== userToDelete.email.toLowerCase()}
				data-testid="confirm-delete-user-btn"
				class="rounded-lg bg-status-danger text-white px-4 py-2 text-xs font-semibold hover:opacity-90 disabled:opacity-40"
			>
				{deletingUser ? 'Deleting...' : 'Delete User'}
			</button>
		</div>
	</form>
</dialog>

<style>
	dialog:modal {
		overscroll-behavior: contain;
	}
	dialog::backdrop {
		overflow: hidden;
		overscroll-behavior: contain;
	}
	.admin-tab[aria-selected='true']::after {
		content: '';
		position: absolute;
		inset: auto 0.5rem 0;
		border-block-end: 2px solid currentColor;
	}
</style>
