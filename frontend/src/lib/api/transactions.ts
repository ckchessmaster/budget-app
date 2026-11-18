import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import type {
	AccountItem,
	CreateTransactionPayload,
	SolvencyReport,
	TransactionItemModel
} from '$lib/types/transaction';

function num(val: unknown): number {
	if (typeof val === 'number') return val;
	if (typeof val === 'string') {
		const parsed = Number(val);
		return Number.isFinite(parsed) ? parsed : 0;
	}
	return 0;
}

function authHeaders(): HeadersInit {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	if (auth.token) {
		headers['Authorization'] = `Bearer ${auth.token}`;
	}
	return headers;
}

function mapTransaction(raw: any): TransactionItemModel {
	return {
		id: raw.id,
		accountId: raw.account_id,
		origin: raw.origin ?? 'BANK_FEED',
		status: raw.status ?? 'POSTED',
		amount: num(raw.amount),
		merchantRaw: raw.merchant_raw,
		merchantClean: raw.merchant_clean ?? null,
		categoryId: raw.category_id ?? null,
		transactionDate: String(raw.transaction_date).slice(0, 10),
		userNotes: raw.user_notes ?? null,
		isCleared: Boolean(raw.is_cleared),
		splits: Array.isArray(raw.splits)
			? raw.splits.map((s: any) => ({
					id: s.id,
					amount: num(s.amount),
					categoryId: s.category_id ?? null,
					userNotes: s.user_notes ?? null
				}))
			: undefined
	};
}

export async function fetchTransactions(params?: {
	accountId?: string;
	categoryId?: string;
	status?: string;
	page?: number;
	limit?: number;
}): Promise<{ items: TransactionItemModel[]; total: number }> {
	const { apiBaseUrl } = getAppConfig();
	const qs = new URLSearchParams();
	if (params?.accountId) qs.set('account_id', params.accountId);
	if (params?.categoryId) qs.set('category_id', params.categoryId);
	if (params?.status) qs.set('status', params.status);
	if (params?.page) qs.set('page', String(params.page));
	if (params?.limit) qs.set('limit', String(params.limit));

	const query = qs.toString();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions${query ? `?${query}` : ''}`, {
		headers: authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch transactions (${res.status})`);
	}
	const data = await res.json();
	return {
		items: Array.isArray(data.items) ? data.items.map(mapTransaction) : [],
		total: Number(data.total ?? 0)
	};
}

export async function createTransaction(payload: CreateTransactionPayload): Promise<TransactionItemModel> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify({
			account_id: payload.accountId,
			total_amount: payload.totalAmount,
			merchant_raw: payload.merchantRaw,
			category_id: payload.categoryId ?? null,
			transaction_date: `${payload.transactionDate}T00:00:00Z`,
			user_notes: payload.userNotes ?? '',
			splits: payload.splits?.map((s) => ({
				amount: s.amount,
				category_id: s.categoryId,
				user_notes: s.userNotes ?? ''
			}))
		})
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to create transaction (${res.status})`);
	}
	return mapTransaction(await res.json());
}

export async function deleteTransaction(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to delete transaction (${res.status})`);
	}
}

export async function fetchAccounts(): Promise<AccountItem[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts`, {
		headers: authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch accounts (${res.status})`);
	}
	const raw = await res.json();
	if (!Array.isArray(raw)) return [];
	return raw.map((a: any) => ({
		id: a.id,
		name: a.name,
		type: a.type,
		currentBalance: num(a.current_balance),
		isOnBudget: Boolean(a.is_on_budget),
		cardMode: a.card_mode ?? 'PAY_IN_FULL',
		aprPercentage: num(a.apr_percentage)
	}));
}

export async function fetchSolvencyReport(): Promise<SolvencyReport> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts/solvency`, {
		headers: authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch solvency report (${res.status})`);
	}
	const raw = await res.json();
	return {
		totalLiquidCash: num(raw.total_liquid_cash),
		totalCreditCardDebt: num(raw.total_credit_card_debt),
		solvencyBuffer: num(raw.solvency_buffer),
		isFloatingDebt: Boolean(raw.is_floating_debt),
		floatingDebtAmount: num(raw.floating_debt_amount)
	};
}
