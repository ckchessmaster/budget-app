<script lang="ts">
	import {
		detectPushEnvironment,
		subscribeToPushNotifications,
		type PushEnvironment,
		type PushSubscriptionPayload
	} from '$lib/notifications/push';

	interface Props {
		open: boolean;
		vapidPublicKey: string;
		serverEnabled?: boolean;
		environment?: PushEnvironment;
		onClose: () => void;
		onDismiss?: () => void;
		onSubscribed?: (payload: PushSubscriptionPayload) => void;
	}

	let {
		open,
		vapidPublicKey,
		serverEnabled = true,
		environment,
		onClose,
		onDismiss,
		onSubscribed
	}: Props = $props();

	let submitting = $state(false);
	let errorMsg = $state<string | null>(null);
	let status = $state<'idle' | 'subscribed' | 'denied'>('idle');

	let env = $derived(environment ?? detectPushEnvironment());
	let isConfigured = $derived(serverEnabled && vapidPublicKey.trim().length > 0);
	let isDenied = $derived(status === 'denied' || env.permission === 'denied');

	$effect(() => {
		if (!open) {
			submitting = false;
			errorMsg = null;
			status = 'idle';
		}
	});

	function handleClose() {
		submitting = false;
		errorMsg = null;
		status = 'idle';
		onClose();
	}

	function handleNotNow() {
		onDismiss?.();
		handleClose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (open && e.key === 'Escape' && !submitting) {
			e.preventDefault();
			handleClose();
		}
	}

	async function handleEnableAlerts() {
		submitting = true;
		errorMsg = null;
		try {
			const result = await subscribeToPushNotifications(vapidPublicKey);
			if (result.status === 'subscribed') {
				status = 'subscribed';
				onSubscribed?.(result.payload);
			} else if (result.status === 'denied') {
				status = 'denied';
			}
		} catch (err) {
			errorMsg = err instanceof Error ? err.message : 'Failed to enable push alerts.';
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
		role="dialog"
		aria-modal="true"
		aria-labelledby="push-modal-title"
		data-testid="push-prompt-modal"
	>
		<div class="w-full max-w-md bg-surface border border-border text-ink-primary rounded-xl shadow-2xl p-5 space-y-4">
			<div class="flex items-start justify-between gap-3">
				<div class="space-y-1">
					<h3 id="push-modal-title" class="text-base font-semibold text-ink-primary">
						Budget &amp; Statement Alerts
					</h3>
					<p class="text-xs text-ink-secondary">
						Get notified when a category passes 80% spend or a credit card statement is due soon.
					</p>
				</div>
				<button
					type="button"
					onclick={handleClose}
					disabled={submitting}
					aria-label="Close"
					class="text-ink-muted hover:text-ink-primary hover:bg-surface-hover text-sm px-2 py-1 rounded-lg transition-colors"
				>
					✕
				</button>
			</div>

			{#if errorMsg}
				<div
					role="alert"
					class="bg-status-danger-bg border border-status-danger/30 text-status-danger text-xs px-3 py-2 rounded-md"
				>
					{errorMsg}
				</div>
			{/if}

			{#if !isConfigured}
				<div class="rounded-lg border border-border bg-subtle p-3 text-xs text-ink-secondary space-y-1">
					<p class="font-medium text-ink-primary">Push notifications are unavailable</p>
					<p>
						VAPID keys are not configured on this server yet. Contact your administrator to enable Web Push alerts.
					</p>
				</div>
				<div class="flex justify-end">
					<button
						type="button"
						onclick={handleClose}
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Close
					</button>
				</div>
			{:else if env.supportReason === 'ios_requires_install'}
				<div
					data-testid="ios-install-guide"
					class="rounded-lg border border-border bg-subtle p-3.5 text-xs text-ink-secondary space-y-2.5"
				>
					<p class="font-semibold text-ink-primary">
						Install Budget App to your Home Screen first
					</p>
					<p>
						iOS Safari only enables Web Push notifications for apps launched from the Home Screen:
					</p>
					<ol class="list-decimal list-inside space-y-1 text-ink-primary">
						<li>Tap the <strong>Share</strong> button in Safari.</li>
						<li>Select <strong>Add to Home Screen</strong> and tap <strong>Add</strong>.</li>
						<li>Open Budget App from your Home Screen and tap <strong>Enable Alerts</strong>.</li>
					</ol>
				</div>
				<div class="flex justify-end gap-2">
					<button
						type="button"
						onclick={handleNotNow}
						data-testid="push-not-now-btn"
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Got It
					</button>
				</div>
			{:else if env.supportReason === 'unsupported_browser'}
				<div class="rounded-lg border border-border bg-subtle p-3 text-xs text-ink-secondary space-y-1">
					<p class="font-medium text-ink-primary">Browser does not support Web Push</p>
					<p>
						This browser does not expose Service Worker PushManager and Notification APIs.
					</p>
				</div>
				<div class="flex justify-end">
					<button
						type="button"
						onclick={handleClose}
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Close
					</button>
				</div>
			{:else if status === 'subscribed'}
				<div
					data-testid="push-subscribed-success"
					class="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-xs text-emerald-700 dark:text-emerald-300 space-y-1"
				>
					<p class="font-semibold">Alerts enabled on this device</p>
					<p>
						You will receive spending velocity alerts (&ge;80% budget) and credit card statement due reminders (&le;5 days).
					</p>
				</div>
				<div class="flex justify-end">
					<button
						type="button"
						onclick={handleClose}
						class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 text-accent-primary-text rounded-md transition-opacity"
					>
						Done
					</button>
				</div>
			{:else if isDenied}
				<div
					role="alert"
					class="rounded-lg border border-status-danger/30 bg-status-danger-bg p-3 text-xs text-status-danger space-y-1"
				>
					<p class="font-semibold">Notifications are blocked</p>
					<p>
						Permission was denied. Open your device or browser site settings for Budget App to allow notifications, then return to Settings to subscribe.
					</p>
				</div>
				<div class="flex justify-end">
					<button
						type="button"
						onclick={handleNotNow}
						data-testid="push-not-now-btn"
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Close
					</button>
				</div>
			{:else}
				<div class="rounded-lg border border-border bg-subtle p-3 text-xs text-ink-secondary space-y-1.5">
					<p class="font-medium text-ink-primary">What you will receive:</p>
					<ul class="list-disc list-inside space-y-1">
						<li>Spending velocity warnings when a category reaches 80% of its monthly budget.</li>
						<li>Credit card statement reminders when a due date is within 5 days and checking funds are low.</li>
					</ul>
				</div>

				<div class="flex justify-end gap-2 pt-1">
					<button
						type="button"
						onclick={handleNotNow}
						disabled={submitting}
						data-testid="push-not-now-btn"
						class="px-3 py-1.5 text-xs border border-border bg-subtle text-ink-secondary hover:bg-surface-hover hover:text-ink-primary rounded-md transition-colors"
					>
						Not Now
					</button>
					<button
						type="button"
						onclick={() => void handleEnableAlerts()}
						disabled={submitting}
						data-testid="enable-alerts-btn"
						class="px-4 py-1.5 text-xs font-medium bg-accent-primary hover:opacity-90 disabled:opacity-50 text-accent-primary-text rounded-md transition-opacity"
					>
						{submitting ? 'Enabling...' : 'Enable Alerts'}
					</button>
				</div>
			{/if}
		</div>
	</div>
{/if}
