<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import {
		acceptInvitation,
		ApiError,
		previewInvitation,
		type InvitePreviewResponse
	} from '$lib/api/workspaces';
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import { setPendingJoinedWorkspace, workspace } from '$lib/stores/workspace.svelte';

	let token = $derived((page.url.searchParams.get('token') || '').trim());
	let loading = $state(true);
	let submitting = $state(false);
	let preview = $state<InvitePreviewResponse | null>(null);
	let errorCode = $state<string | null>(null);
	let errorMessage = $state<string | null>(null);
	let nameInput = $state('');

	function roleLabel(role: string): string {
		return role === 'read_only' ? 'Read-Only Viewer' : 'Household Member';
	}

	function mapAcceptError(err: unknown): { code: string; message: string } {
		if (err instanceof ApiError) {
			if (err.status === 410 || err.code === 'invitation_expired') {
				return {
					code: 'expired',
					message: 'This invitation link has expired. Ask the workspace owner to resend a fresh link.'
				};
			}
			if (err.status === 409 || err.code.startsWith('invitation_already_')) {
				return {
					code: 'unavailable',
					message:
						err.code === 'invitation_already_accepted'
							? 'This invitation has already been accepted.'
							: 'This invitation has been revoked by the workspace owner.'
				};
			}
			if (err.status === 403 || err.code === 'email_mismatch') {
				return {
					code: 'email_mismatch',
					message: 'You are signed in with a different email address than the one invited.'
				};
			}
			if (err.status === 404 || err.code === 'invitation_not_found') {
				return {
					code: 'not_found',
					message: 'This invitation link is invalid or could not be found.'
				};
			}
			return { code: err.code, message: err.message };
		}
		return {
			code: 'unknown',
			message: err instanceof Error ? err.message : 'Unable to process invitation.'
		};
	}

	async function loadPreview() {
		if (!token) {
			loading = false;
			errorCode = 'missing_token';
			errorMessage = 'No invitation token was found in this link.';
			return;
		}
		loading = true;
		errorCode = null;
		errorMessage = null;
		try {
			await auth.ready();
			const res = await previewInvitation(token);
			preview = res;
			if (res.status === 'EXPIRED') {
				errorCode = 'expired';
				errorMessage = 'This invitation link has expired. Ask the workspace owner to resend a fresh link.';
			} else if (res.status === 'ACCEPTED') {
				errorCode = 'unavailable';
				errorMessage = 'This invitation has already been accepted.';
			} else if (res.status === 'REVOKED') {
				errorCode = 'unavailable';
				errorMessage = 'This invitation has been revoked by the workspace owner.';
			} else if (res.email_mismatch) {
				errorCode = 'email_mismatch';
				errorMessage = `You are signed in as ${res.signed_in_email}, but this invitation was sent to ${res.email}.`;
			}
		} catch (err) {
			const mapped = mapAcceptError(err);
			errorCode = mapped.code;
			errorMessage = mapped.message;
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void loadPreview();
	});

	async function handleAccept(e: SubmitEvent) {
		e.preventDefault();
		if (!token) return;
		submitting = true;
		errorCode = null;
		errorMessage = null;
		try {
			const res = await acceptInvitation(token, nameInput.trim());
			setPendingJoinedWorkspace(res.workspace_id);

			if (res.is_new_user && res.redirect_url) {
				window.location.assign(res.redirect_url);
				return;
			}
			if (auth.isAuthenticated) {
				await workspace.load();
				await demo.load().catch(() => {});
				demo.dataVersion += 1;
				await goto('/budget');
				return;
			}
			await auth.login();
		} catch (err) {
			const mapped = mapAcceptError(err);
			errorCode = mapped.code;
			errorMessage = mapped.message;
			submitting = false;
		}
	}

	async function handleSwitchAccount() {
		workspace.resetOnLogout();
		demo.resetOnLogout();
		await auth.logout();
		await auth.login();
	}
</script>

<div class="min-h-full flex items-center justify-center p-4">
	<div
		data-testid="invite-card"
		class="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl space-y-5"
	>
		<div class="text-center space-y-2">
			<div class="inline-flex w-11 h-11 rounded-xl bg-accent-primary text-accent-primary-text items-center justify-center font-bold text-xl">
				$
			</div>
			<h2 class="text-xl font-bold text-ink-primary">Household Invitation</h2>
			{#if preview}
				<p class="text-xs text-ink-secondary">
					You've been invited to join <strong class="text-ink-primary">{preview.workspace_name}</strong>
					as a <strong class="text-ink-primary">{roleLabel(preview.role)}</strong>
				</p>
			{/if}
		</div>

		{#if loading}
			<div class="py-6 text-center text-xs text-ink-secondary">Checking invitation...</div>
		{:else if errorMessage}
			<div
				data-testid="invite-error"
				class="rounded-xl border border-status-danger/30 bg-status-danger-bg p-4 text-xs text-status-danger space-y-3"
			>
				<p class="font-medium">{errorMessage}</p>
				{#if errorCode === 'email_mismatch'}
					<button
						type="button"
						onclick={() => void handleSwitchAccount()}
						data-testid="switch-account-btn"
						class="w-full rounded-lg bg-accent-primary text-accent-primary-text py-2 px-3 text-xs font-semibold hover:opacity-90"
					>
						Sign Out &amp; Switch Account
					</button>
				{:else}
					<a
						href="/login"
						class="inline-block rounded-lg border border-border bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary hover:bg-surface-hover"
					>
						Go to Sign In
					</a>
				{/if}
			</div>
		{:else if preview}
			<div class="rounded-xl border border-border bg-subtle p-3.5 text-xs space-y-1">
				<div class="flex items-center justify-between gap-2">
					<span class="text-ink-secondary">Invited email</span>
					<span class="font-mono font-medium text-ink-primary">{preview.email}</span>
				</div>
				{#if preview.invited_by_email}
					<div class="flex items-center justify-between gap-2">
						<span class="text-ink-secondary">Invited by</span>
						<span class="font-medium text-ink-primary">{preview.invited_by_email}</span>
					</div>
				{/if}
			</div>

			<form onsubmit={handleAccept} class="space-y-4">
				{#if !auth.isAuthenticated}
					<div>
						<label for="invite-name-input" class="block text-xs font-medium text-ink-primary mb-1">
							Your Name
						</label>
						<input
							id="invite-name-input"
							type="text"
							bind:value={nameInput}
							placeholder="Jane Kingdon"
							required
							class="w-full rounded-lg border border-border bg-subtle px-3 py-2 text-sm text-ink-primary"
						/>
					</div>
				{/if}

				<button
					type="submit"
					disabled={submitting || (!auth.isAuthenticated && !nameInput.trim())}
					data-testid="accept-invite-btn"
					class="w-full py-2.5 px-4 rounded-lg bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text text-sm font-semibold transition-opacity"
				>
					{submitting ? 'Joining Workspace...' : 'Accept Invitation'}
				</button>
			</form>
		{/if}
	</div>
</div>
