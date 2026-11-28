<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { redeemPlatformInvite, validatePlatformInvite } from '$lib/api/admin';
	import { ApiError } from '$lib/api/workspaces';

	let token = $derived((page.url.searchParams.get('invite') || '').trim());
	let loading = $state(true);
	let submitting = $state(false);
	let inviteValid = $state(false);
	let expiresAt = $state('');
	let inviteError = $state<string | null>(null);
	let submitError = $state<string | null>(null);

	let nameInput = $state('');
	let emailInput = $state('');
	let usernameInput = $state('');

	function mapSignupError(err: unknown): string {
		if (err instanceof ApiError) {
			if (err.status === 410 || err.code === 'invitation_expired') {
				return 'This invite link has expired. Ask your administrator for a fresh invite link.';
			}
			if (err.code === 'invitation_already_redeemed') {
				return 'This invite link has already been redeemed.';
			}
			if (err.code === 'invitation_revoked') {
				return 'This invite link has been revoked by an administrator.';
			}
			if (err.status === 404 || err.code === 'invitation_not_found') {
				return 'This invite link is invalid or could not be found.';
			}
			if (err.code === 'email_already_registered') {
				return 'An account with this email address already exists. Please sign in instead.';
			}
			if (err.code === 'username_already_taken') {
				return 'That username is already taken. Choose a different username.';
			}
			if (err.code === 'invalid_username') {
				return 'Username must be 3–64 characters using letters, numbers, dots, underscores, or hyphens.';
			}
			if (err.code === 'invalid_email') {
				return 'Enter a valid email address in the format you@example.com.';
			}
			return err.message;
		}
		return err instanceof Error ? err.message : 'Unable to process invitation.';
	}

	async function checkInvite() {
		if (!token) {
			loading = false;
			inviteValid = false;
			inviteError = 'No invite token was provided in this URL.';
			return;
		}
		loading = true;
		inviteError = null;
		try {
			const res = await validatePlatformInvite(token);
			inviteValid = Boolean(res.valid);
			expiresAt = res.expires_at;
			if (res.email && !emailInput) {
				emailInput = res.email;
			}
		} catch (err) {
			inviteValid = false;
			inviteError = mapSignupError(err);
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void checkInvite();
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!token || !inviteValid) return;

		submitting = true;
		submitError = null;
		try {
			const res = await redeemPlatformInvite({
				token,
				name: nameInput.trim(),
				email: emailInput.trim(),
				username: usernameInput.trim()
			});
			window.location.assign(res.setup_url);
		} catch (err) {
			const mapped = mapSignupError(err);
			if (
				err instanceof ApiError &&
				(err.status === 410 ||
					err.code === 'invitation_already_redeemed' ||
					err.code === 'invitation_revoked' ||
					err.status === 404)
			) {
				inviteValid = false;
				inviteError = mapped;
			} else {
				submitError = mapped;
			}
			submitting = false;
		}
	}
</script>

<div class="min-h-full flex items-center justify-center p-4">
	<div
		data-testid="signup-card"
		class="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl space-y-5"
	>
		<div class="text-center space-y-2">
			<div class="inline-flex w-11 h-11 rounded-xl bg-accent-primary text-accent-primary-text items-center justify-center font-bold text-xl">
				$
			</div>
			<h2 class="text-xl font-bold text-ink-primary">Create Your Account</h2>
			<p class="text-xs text-ink-secondary">
				Redeem your single-use Budget App invitation to set up your credentials.
			</p>
		</div>

		{#if loading}
			<div class="py-6 text-center text-xs text-ink-secondary">Validating invite link...</div>
		{:else if inviteError || !inviteValid}
			<div
				data-testid="signup-invite-error"
				class="rounded-xl border border-status-danger/30 bg-status-danger-bg p-4 text-xs text-status-danger space-y-3"
			>
				<p class="font-medium">Error: {inviteError || 'This invite link is invalid.'}</p>
				<a
					href="/login"
					class="inline-block rounded-lg border border-border bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary hover:bg-surface-hover"
				>
					Go to Sign In
				</a>
			</div>
		{:else}
			{#if expiresAt}
				<p class="text-[11px] text-center text-ink-secondary">
					Invite expires {new Date(expiresAt).toLocaleString()}
				</p>
			{/if}

			{#if submitError}
				<div
					data-testid="signup-submit-error"
					role="alert"
					class="rounded-xl border border-status-danger/30 bg-status-danger-bg p-3 text-xs text-status-danger font-medium"
				>
					Error: {submitError}
				</div>
			{/if}

			<form onsubmit={handleSubmit} class="space-y-4">
				<div>
					<label for="signup-name" class="block text-xs font-medium text-ink-primary mb-1">
						Full Name
					</label>
					<input
						id="signup-name"
						name="name"
						type="text"
						autocomplete="name"
						enterkeyhint="next"
						required
						maxlength="255"
						placeholder="Mike Johnson"
						bind:value={nameInput}
						class="signup-input w-full rounded-lg border border-border bg-subtle px-3 py-2 text-sm text-ink-primary"
					/>
				</div>

				<div>
					<label for="signup-email" class="block text-xs font-medium text-ink-primary mb-1">
						Email Address
					</label>
					<span id="signup-email-hint" class="block text-[11px] text-ink-secondary mb-1">
						Format: you@example.com
					</span>
					<input
						id="signup-email"
						name="email"
						type="email"
						autocomplete="email"
						spellcheck="false"
						enterkeyhint="next"
						aria-describedby="signup-email-hint"
						required
						placeholder="mike@example.com"
						bind:value={emailInput}
						class="signup-input w-full rounded-lg border border-border bg-subtle px-3 py-2 text-sm text-ink-primary"
					/>
				</div>

				<div>
					<label for="signup-username" class="block text-xs font-medium text-ink-primary mb-1">
						Username
					</label>
					<span id="signup-username-hint" class="block text-[11px] text-ink-secondary mb-1">
						3–64 characters: letters, numbers, dots, underscores, or hyphens
					</span>
					<input
						id="signup-username"
						name="username"
						type="text"
						autocomplete="username"
						spellcheck="false"
						autocapitalize="none"
						autocorrect="off"
						pattern={'[a-zA-Z0-9._-]{3,64}'}
						enterkeyhint="done"
						aria-describedby="signup-username-hint"
						required
						placeholder="mike"
						bind:value={usernameInput}
						class="signup-input w-full rounded-lg border border-border bg-subtle px-3 py-2 text-sm text-ink-primary"
					/>
				</div>

				<button
					type="submit"
					disabled={submitting}
					data-testid="signup-submit-btn"
					class="w-full py-2.5 px-4 rounded-lg bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text text-sm font-semibold transition-opacity"
				>
					{submitting ? 'Creating Account...' : 'Create Account'}
				</button>
			</form>
		{/if}
	</div>
</div>

<style>
	.signup-input:user-invalid {
		border-color: rgb(225 29 72);
	}
	.signup-input:user-valid {
		border-color: rgb(5 150 105);
	}
</style>
