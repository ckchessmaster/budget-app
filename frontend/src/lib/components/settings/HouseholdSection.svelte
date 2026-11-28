<script lang="ts">
	import {
		createWorkspaceInvitation,
		fetchWorkspaceInvitations,
		fetchWorkspaceMembers,
		removeWorkspaceMember,
		resendWorkspaceInvitation,
		revokeWorkspaceInvitation,
		updateMemberRole,
		type WorkspaceInvitation,
		type WorkspaceMember
	} from '$lib/api/workspaces';
	import { demo } from '$lib/stores/demo.svelte';
	import { workspace } from '$lib/stores/workspace.svelte';

	let members = $state<WorkspaceMember[]>([]);
	let invitations = $state<WorkspaceInvitation[]>([]);
	let loading = $state(false);
	let errorMsg = $state<string | null>(null);
	let feedbackMsg = $state<string | null>(null);

	let inviteEmail = $state('');
	let inviteRole = $state<'member' | 'read_only'>('member');
	let sendingInvite = $state(false);

	let busyId = $state<string | null>(null);
	let confirmRemoveUserId = $state<string | null>(null);

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

	function formatExpiry(iso: string, status: string): string {
		if (status === 'EXPIRED') return 'Expired';
		const ms = new Date(iso).getTime() - Date.now();
		if (Number.isNaN(ms) || ms <= 0) return 'Expired';
		const days = Math.floor(ms / (24 * 60 * 60 * 1000));
		if (days >= 1) return `Expires in ${days}d`;
		const hours = Math.max(1, Math.ceil(ms / (60 * 60 * 1000)));
		return `Expires in ${hours}h`;
	}

	function friendlyError(err: unknown, fallback: string): string {
		const msg = err instanceof Error ? err.message : fallback;
		switch (msg) {
			case 'already_member':
				return 'That email is already a member of this workspace.';
			case 'invite_already_pending':
				return 'A pending invitation already exists for that email. Use Resend below instead.';
			case 'cannot_invite_to_demo_workspace':
				return 'Exit Demo Mode before inviting household members.';
			case 'invalid_email':
				return 'Please enter a valid email address.';
			default:
				return msg || fallback;
		}
	}

	export async function loadHousehold() {
		if (demo.isDemo) {
			members = [];
			invitations = [];
			return;
		}
		loading = true;
		errorMsg = null;
		try {
			const [loadedMembers, loadedInvites] = await Promise.all([
				fetchWorkspaceMembers(),
				workspace.isOwner ? fetchWorkspaceInvitations().catch(() => []) : Promise.resolve([])
			]);
			members = loadedMembers;
			invitations = loadedInvites;
		} catch (err) {
			errorMsg = friendlyError(err, 'Failed to load household members.');
		} finally {
			loading = false;
		}
	}

	let lastKey = $state('');
	$effect(() => {
		const key = `${workspace.activeWorkspaceId}:${workspace.currentRole}:${demo.isDemo}`;
		if (key !== lastKey && workspace.loaded) {
			lastKey = key;
			void loadHousehold();
		}
	});

	async function handleSendInvite(e: SubmitEvent) {
		e.preventDefault();
		const email = inviteEmail.trim();
		if (!email) return;
		sendingInvite = true;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const created = await createWorkspaceInvitation(email, inviteRole);
			invitations = [created, ...invitations];
			inviteEmail = '';
			feedbackMsg = `Invitation dispatched to ${created.email}.`;
		} catch (err) {
			errorMsg = friendlyError(err, 'Failed to send invitation.');
		} finally {
			sendingInvite = false;
		}
	}

	async function handleResend(inv: WorkspaceInvitation) {
		busyId = inv.id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const updated = await resendWorkspaceInvitation(inv.id);
			invitations = invitations.map((item) => (item.id === inv.id ? updated : item));
			feedbackMsg = `Rotated invite link and resent email to ${updated.email}.`;
		} catch (err) {
			errorMsg = friendlyError(err, 'Failed to resend invitation.');
		} finally {
			busyId = null;
		}
	}

	async function handleRevoke(inv: WorkspaceInvitation) {
		busyId = inv.id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			await revokeWorkspaceInvitation(inv.id);
			invitations = invitations.filter((item) => item.id !== inv.id);
			feedbackMsg = `Revoked invitation for ${inv.email}.`;
		} catch (err) {
			errorMsg = friendlyError(err, 'Failed to revoke invitation.');
		} finally {
			busyId = null;
		}
	}

	async function handleRoleChange(member: WorkspaceMember, newRole: 'member' | 'read_only') {
		busyId = member.user_id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			const updated = await updateMemberRole(member.user_id, newRole);
			members = members.map((m) => (m.user_id === member.user_id ? updated : m));
			feedbackMsg = `Updated ${updated.display_name || updated.email} to ${roleLabel(updated.role)}.`;
		} catch (err) {
			errorMsg = friendlyError(err, 'Failed to update member role.');
		} finally {
			busyId = null;
		}
	}

	async function handleRemoveMember(member: WorkspaceMember) {
		busyId = member.user_id;
		errorMsg = null;
		feedbackMsg = null;
		try {
			await removeWorkspaceMember(member.user_id);
			confirmRemoveUserId = null;
			if (member.is_current_user) {
				await workspace.load();
				await demo.load().catch(() => {});
				demo.dataVersion += 1;
				return;
			}
			members = members.filter((m) => m.user_id !== member.user_id);
			feedbackMsg = `Removed ${member.display_name || member.email} from workspace.`;
		} catch (err) {
			errorMsg = friendlyError(err, 'Failed to remove member.');
		} finally {
			busyId = null;
		}
	}
</script>

<section
	id="household"
	data-testid="household-section"
	class="rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-5 space-y-5 shadow-xs"
>
	<div class="flex flex-wrap items-center justify-between gap-2">
		<div>
			<h3 class="text-base font-bold text-slate-900 dark:text-white">Household Sharing</h3>
			<p class="text-xs text-slate-500 dark:text-slate-400">
				Share your active budget workspace with family members and manage access roles
			</p>
		</div>
		<span
			data-testid="household-role-badge"
			class="px-2.5 py-0.5 rounded-full text-[11px] font-semibold {roleBadgeClass(workspace.currentRole)}"
		>
			Your Role: {roleLabel(workspace.currentRole)}
		</span>
	</div>

	{#if demo.isDemo}
		<p class="text-xs text-amber-800 dark:text-amber-200 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg px-3 py-2">
			Demo sandboxes are private to your account. Exit Demo Mode to invite or manage household members on a real workspace.
		</p>
	{:else}
		{#if errorMsg}
			<div class="rounded-lg border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">
				{errorMsg}
			</div>
		{/if}
		{#if feedbackMsg}
			<div class="rounded-lg border border-emerald-200 dark:border-emerald-500/30 bg-emerald-50 dark:bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-300">
				{feedbackMsg}
			</div>
		{/if}

		<!-- Members List -->
		<div class="space-y-2">
			<h4 class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
				Workspace Members ({members.length})
			</h4>
			{#if loading && members.length === 0}
				<p class="text-xs text-slate-500 dark:text-slate-400">Loading members...</p>
			{:else if members.length === 0}
				<p class="text-xs text-slate-500 dark:text-slate-400">No members found.</p>
			{:else}
				<div class="divide-y divide-slate-100 dark:divide-slate-800 rounded-xl border border-slate-200 dark:border-slate-800">
					{#each members as member (member.user_id)}
						<div
							data-testid="household-member-row"
							class="flex flex-wrap items-center justify-between gap-3 px-3.5 py-2.5"
						>
							<div class="min-w-0">
								<div class="flex items-center gap-2">
									<span class="text-xs font-semibold text-slate-900 dark:text-slate-100 truncate">
										{member.display_name || member.email || member.user_id.slice(0, 8)}
									</span>
									{#if member.is_current_user}
										<span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300">
											You
										</span>
									{/if}
								</div>
								{#if member.email}
									<p class="text-[11px] text-slate-500 dark:text-slate-400 truncate">{member.email}</p>
								{/if}
							</div>

							<div class="flex items-center gap-2 shrink-0">
								{#if workspace.isOwner && member.role !== 'owner' && !member.is_current_user}
									<select
										aria-label="Member role"
										value={member.role}
										disabled={busyId === member.user_id}
										onchange={(e) =>
											void handleRoleChange(
												member,
												(e.currentTarget as HTMLSelectElement).value as 'member' | 'read_only'
											)}
										class="rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-2 py-1 text-xs text-slate-800 dark:text-slate-200"
									>
										<option value="member">Member</option>
										<option value="read_only">Read-Only</option>
									</select>
								{:else}
									<span class="px-2 py-0.5 rounded-full text-[11px] font-semibold {roleBadgeClass(member.role)}">
										{roleLabel(member.role)}
									</span>
								{/if}

								{#if (workspace.isOwner && member.role !== 'owner' && !member.is_current_user) || (!workspace.isOwner && member.is_current_user)}
									{#if confirmRemoveUserId === member.user_id}
										<button
											type="button"
											disabled={busyId === member.user_id}
											onclick={() => void handleRemoveMember(member)}
											class="px-2.5 py-1 rounded-lg text-xs font-semibold bg-rose-600 hover:bg-rose-500 text-white"
										>
											{member.is_current_user ? 'Confirm Leave' : 'Confirm Remove'}
										</button>
										<button
											type="button"
											onclick={() => (confirmRemoveUserId = null)}
											class="px-2 py-1 text-xs text-slate-500 hover:text-slate-700 dark:text-slate-400"
										>
											Cancel
										</button>
									{:else}
										<button
											type="button"
											data-testid={member.is_current_user ? 'leave-workspace-btn' : `remove-member-${member.user_id}`}
											onclick={() => (confirmRemoveUserId = member.user_id)}
											class="px-2.5 py-1 rounded-lg text-xs font-medium text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10"
										>
											{member.is_current_user ? 'Leave Workspace' : 'Remove'}
										</button>
									{/if}
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>

		<!-- Owner-Only Invite Form & Pending Invitations -->
		{#if workspace.isOwner}
			<div class="pt-3 border-t border-slate-100 dark:border-slate-800 space-y-3">
				<h4 class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
					Invite Family Member
				</h4>
				<form onsubmit={handleSendInvite} class="flex flex-wrap items-end gap-2">
					<div class="flex-1 min-w-[200px]">
						<label for="invite-email-input" class="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">
							Email Address
						</label>
						<input
							id="invite-email-input"
							type="email"
							bind:value={inviteEmail}
							placeholder="family@example.com"
							required
							class="w-full rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-1.5 text-xs text-slate-900 dark:text-slate-100"
						/>
					</div>
					<div>
						<label for="invite-role-select" class="block text-xs font-medium text-slate-700 dark:text-slate-300 mb-1">
							Role
						</label>
						<select
							id="invite-role-select"
							bind:value={inviteRole}
							class="rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-950 px-3 py-1.5 text-xs text-slate-900 dark:text-slate-100"
						>
							<option value="member">Member (Edit Budgets &amp; Transactions)</option>
							<option value="read_only">Read-Only (View Only)</option>
						</select>
					</div>
					<button
						type="submit"
						disabled={sendingInvite || !inviteEmail.trim()}
						data-testid="send-invite-btn"
						class="px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white"
					>
						{sendingInvite ? 'Sending...' : 'Send Invite'}
					</button>
				</form>
			</div>

			{#if invitations.length > 0}
				<div class="pt-3 border-t border-slate-100 dark:border-slate-800 space-y-2">
					<h4 class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
						Pending Invitations ({invitations.length})
					</h4>
					<div class="divide-y divide-slate-100 dark:divide-slate-800 rounded-xl border border-slate-200 dark:border-slate-800">
						{#each invitations as inv (inv.id)}
							<div
								data-testid="pending-invite-row"
								class="flex flex-wrap items-center justify-between gap-3 px-3.5 py-2.5"
							>
								<div class="min-w-0 space-y-0.5">
									<div class="flex items-center gap-2">
										<span class="text-xs font-semibold text-slate-900 dark:text-slate-100 truncate">
											{inv.email}
										</span>
										<span class="px-2 py-0.5 rounded-full text-[10px] font-semibold {roleBadgeClass(inv.role)}">
											{roleLabel(inv.role)}
										</span>
									</div>
									<p class="text-[11px] {inv.status === 'EXPIRED' ? 'text-rose-600 dark:text-rose-400 font-medium' : 'text-slate-500 dark:text-slate-400'}">
										{formatExpiry(inv.expires_at, inv.status)}
									</p>
								</div>

								<div class="flex items-center gap-1.5 shrink-0">
									<button
										type="button"
										disabled={busyId === inv.id}
										onclick={() => void handleResend(inv)}
										data-testid="resend-invite-{inv.id}"
										class="px-2.5 py-1 rounded-lg text-xs font-medium border border-slate-200 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-200 disabled:opacity-50"
									>
										Resend
									</button>
									<button
										type="button"
										disabled={busyId === inv.id}
										onclick={() => void handleRevoke(inv)}
										data-testid="revoke-invite-{inv.id}"
										class="px-2.5 py-1 rounded-lg text-xs font-medium text-rose-600 dark:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10 disabled:opacity-50"
									>
										Revoke
									</button>
								</div>
							</div>
						{/each}
					</div>
				</div>
			{/if}
		{/if}
	{/if}
</section>
