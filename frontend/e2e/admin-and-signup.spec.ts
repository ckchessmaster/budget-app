import { expect, test, type Page } from '@playwright/test';

const OIDC_STORAGE_KEY = 'oidc.user:http://localhost:5556/dex:budget-app-client';

async function seedOidcSession(page: Page, opts: { userId: string; email: string; groups: string[] }) {
	const header = Buffer.from(JSON.stringify({ alg: 'RS256', typ: 'JWT' })).toString('base64url');
	const payload = Buffer.from(
		JSON.stringify({
			sub: opts.userId,
			email: opts.email,
			groups: opts.groups,
			exp: Math.floor(Date.now() / 1000) + 3600
		})
	).toString('base64url');
	const jwt = `${header}.${payload}.fakesig`;

	const userPayload = JSON.stringify({
		access_token: jwt,
		id_token: jwt,
		token_type: 'Bearer',
		scope: 'openid profile email groups offline_access',
		expires_at: Math.floor(Date.now() / 1000) + 3600,
		profile: {
			sub: opts.userId,
			email: opts.email,
			name: opts.email,
			groups: opts.groups
		}
	});

	await page.addInitScript(
		([key, val]) => {
			window.localStorage.setItem(key, val);
		},
		[OIDC_STORAGE_KEY, userPayload]
	);
}

interface InviteRecord {
	id: string;
	label: string;
	token: string;
	status: 'ACTIVE' | 'REDEEMED' | 'REVOKED' | 'EXPIRED';
	expires_at: string;
	created_at: string;
	redeemed_by_email?: string;
	redeemed_at?: string;
}

async function setupApiMocks(page: Page, opts: { isAdmin: boolean; userId: string; email: string }) {
	const invites = new Map<string, InviteRecord>();

	await page.route('http://localhost:8080/api/v1/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		const method = route.request().method();

		if (path === '/api/v1/auth/me') {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					user_id: opts.userId,
					email: opts.email,
					is_platform_admin: opts.isAdmin,
					is_active: true
				})
			});
		}

		if (path === '/api/v1/workspaces/provision' || path === '/api/v1/workspaces/me') {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					workspace_id: 'ws-personal',
					workspace_name: 'Personal Workspace',
					role: 'owner',
					is_demo: false,
					demo_mode_enabled: false
				})
			});
		}
		if (path === '/api/v1/workspaces') {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([
					{ id: 'ws-personal', name: 'Personal Workspace', role: 'owner', is_demo: false }
				])
			});
		}
		if (path === '/api/v1/demo/status') {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ demo_mode_enabled: false })
			});
		}
		if (path === '/api/v1/notifications/config') {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ enabled: false })
			});
		}
		if (path.startsWith('/api/v1/budget') || path.startsWith('/api/v1/accounts') || path.startsWith('/api/v1/transactions')) {
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			});
		}

		// Public signup validate & redeem
		if (path === '/api/v1/signup/validate' && method === 'GET') {
			const token = url.searchParams.get('invite') || '';
			const rec = [...invites.values()].find((i) => i.token === token);
			if (!rec) {
				return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'invitation_not_found' }) });
			}
			if (rec.status === 'REDEEMED') {
				return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: 'invitation_already_redeemed' }) });
			}
			if (rec.status === 'REVOKED') {
				return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: 'invitation_revoked' }) });
			}
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ valid: true, expires_at: rec.expires_at })
			});
		}

		if (path === '/api/v1/signup/redeem' && method === 'POST') {
			const body = route.request().postDataJSON() as { token: string; email: string; name: string; username: string };
			const rec = [...invites.values()].find((i) => i.token === body.token);
			if (!rec) {
				return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'invitation_not_found' }) });
			}
			if (rec.status === 'REDEEMED') {
				return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: 'invitation_already_redeemed' }) });
			}
			rec.status = 'REDEEMED';
			rec.redeemed_by_email = body.email;
			rec.redeemed_at = new Date().toISOString();
			return route.fulfill({
				status: 201,
				contentType: 'application/json',
				body: JSON.stringify({ setup_url: 'http://127.0.0.1:5173/login?setup=complete' })
			});
		}

		// Admin endpoints
		if (path.startsWith('/api/v1/admin/')) {
			if (!opts.isAdmin) {
				return route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: 'platform_admin_required' }) });
			}
			if (path === '/api/v1/admin/invitations' && method === 'GET') {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([...invites.values()])
				});
			}
			if (path === '/api/v1/admin/invitations' && method === 'POST') {
				const body = route.request().postDataJSON() as { label?: string; expires_in_hours?: number };
				const id = `inv-${invites.size + 1}`;
				const token = `tok-${invites.size + 1}-secret`;
				const rec: InviteRecord = {
					id,
					label: body.label || '',
					token,
					status: 'ACTIVE',
					expires_at: new Date(Date.now() + 7 * 86400_000).toISOString(),
					created_at: new Date().toISOString()
				};
				invites.set(id, rec);
				return route.fulfill({
					status: 201,
					contentType: 'application/json',
					body: JSON.stringify({
						...rec,
						invite_url: `http://127.0.0.1:5173/signup?invite=${token}`
					})
				});
			}
			if (path.startsWith('/api/v1/admin/invitations/') && method === 'DELETE') {
				const id = path.split('/').pop() || '';
				const rec = invites.get(id);
				if (rec) rec.status = 'REVOKED';
				return route.fulfill({ status: 204, body: '' });
			}
			if (path === '/api/v1/admin/users' && method === 'GET') {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([
						{
							user_id: opts.userId,
							email: opts.email,
							display_name: 'Admin User',
							is_active: true,
							created_at: new Date().toISOString(),
							owned_workspace_count: 1,
							member_workspace_count: 0
						}
					])
				});
			}
			if (path === '/api/v1/admin/workspaces' && method === 'GET') {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify([])
				});
			}
			if (path === '/api/v1/admin/health' && method === 'GET') {
				return route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						status: 'ok',
						checked_at: new Date().toISOString(),
						dependencies: {
							postgres: { status: 'ok', latency_ms: 1 },
							temporal: { status: 'ok', latency_ms: 3 }
						},
						simplefin: { configured_workspaces: 0, stale_or_errored: [] },
						temporal_workflows_24h: { failed_bank_syncs: 0, failed_receipt_workflows: 0, failed_invite_emails: 0 },
						outbound_email_24h: { sent: 0, failed: 0 }
					})
				});
			}
		}

		return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
	});
}

test('non-admin user does not see Admin nav link and is redirected from /admin', async ({ page }) => {
	await seedOidcSession(page, {
		userId: '00000000-0000-0000-0000-000000000002',
		email: 'member@dev.local',
		groups: ['users']
	});
	await setupApiMocks(page, {
		isAdmin: false,
		userId: '00000000-0000-0000-0000-000000000002',
		email: 'member@dev.local'
	});

	await page.goto('/settings');
	await expect(page.getByTestId('settings-nav-link')).toBeVisible();
	await expect(page.getByTestId('admin-nav-link')).toHaveCount(0);

	await page.goto('/admin');
	await page.waitForURL((u) => !u.pathname.startsWith('/admin'));
	await expect(page.getByTestId('admin-tab-invites')).toHaveCount(0);
});

test('admin generates single-use invite, revokes another, and unauthenticated friend redeems once', async ({ page, context }) => {
	await seedOidcSession(page, {
		userId: '00000000-0000-0000-0000-000000000001',
		email: 'admin@dev.local',
		groups: ['budget-admins']
	});
	await setupApiMocks(page, {
		isAdmin: true,
		userId: '00000000-0000-0000-0000-000000000001',
		email: 'admin@dev.local'
	});

	await page.goto('/settings');
	await expect(page.getByTestId('admin-nav-link')).toBeVisible();
	await page.getByTestId('admin-nav-link').click();
	await page.waitForURL('**/admin');

	// Generate invite 1 (for friend redemption)
	await page.getByLabel('Label (optional)').fill('For Mike');
	await page.getByTestId('generate-invite-btn').click();
	const inviteURL = (await page.getByTestId('generated-invite-url').textContent())?.trim() || '';
	expect(inviteURL).toContain('/signup?invite=tok-1-secret');

	await page.getByTestId('copy-invite-url-btn').click();
	await page.getByRole('button', { name: 'Close' }).click();

	// Generate invite 2 and revoke it
	await page.getByLabel('Label (optional)').fill('Revoke Test');
	await page.getByTestId('generate-invite-btn').click();
	await page.getByRole('button', { name: 'Close' }).click();
	await page.getByTestId('revoke-invite-inv-2').click();
	await expect(page.getByTestId('invite-row-inv-2')).toContainText('REVOKED');

	// Clear admin session and redeem invite 1 on /signup
	const signupPage = await context.newPage();
	await signupPage.addInitScript(() => window.localStorage.clear());

	// Share the same stateful mock by routing on the context or copying state via a simple API mock on signupPage:
	let redeemed = false;
	await signupPage.route('http://localhost:8080/api/v1/signup/**', async (route) => {
		const url = new URL(route.request().url());
		if (url.pathname === '/api/v1/signup/validate') {
			if (redeemed) {
				return route.fulfill({
					status: 409,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'invitation_already_redeemed' })
				});
			}
			return route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ valid: true, expires_at: new Date(Date.now() + 86400_000).toISOString() })
			});
		}
		if (url.pathname === '/api/v1/signup/redeem') {
			if (redeemed) {
				return route.fulfill({
					status: 409,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'invitation_already_redeemed' })
				});
			}
			redeemed = true;
			return route.fulfill({
				status: 201,
				contentType: 'application/json',
				body: JSON.stringify({ setup_url: 'http://127.0.0.1:5173/login?setup=ok' })
			});
		}
		return route.continue();
	});

	await signupPage.goto('/signup?invite=tok-1-secret');
	await signupPage.getByLabel('Full Name').fill('Mike Johnson');
	await signupPage.getByLabel('Email Address').fill('mike@example.com');
	await signupPage.getByLabel('Username').fill('mikej');
	await signupPage.getByTestId('signup-submit-btn').click();
	await signupPage.waitForURL('**/login?setup=ok');

	// Second visit to the same single-use token fails with already-redeemed state
	await signupPage.goto('/signup?invite=tok-1-secret');
	await expect(signupPage.getByTestId('signup-invite-error')).toContainText('already been redeemed');
});
