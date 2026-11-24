import { auth } from '$lib/stores/auth.svelte';

// The silent renewal iframe lands here. Nothing is rendered and nothing is navigated to:
// the library posts the provider's response back to the page that started the renewal.
// Running the load function is the whole job, so this stays off the interactive callback
// path and never touches the user's history.
export function load(): void {
	void auth.handleSilentCallback();
}
