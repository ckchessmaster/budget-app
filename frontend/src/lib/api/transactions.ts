import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import type {
	Account,
	CreateTransactionPayload,
	SolvencyReport,
	TransactionView
} from '$lib/types/transactions';

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

function mapAccount(raw: any): Account {
	return {
		id: raw.id,
		workspaceId: raw.workspace_id,
		name: raw.name,
		type: raw.type,
		currentBalance: num(raw.current_balance),
		isOnBudget: Boolean(raw.is_on_budget),
		isCreditCard: Boolean(raw.is_credit_card),
		cardMode: raw.card_mode ?? 'PAY_IN_FULL',
		aprPercentage: num(raw.apr_percentage),
		debtPayoffCategoryId: raw.debt_payoff_category_id ?? null
	};
}

function mapTransaction(raw: any): TransactionView {
	return {
		id: raw.id,
		accountId: raw.account_id,
		origin: raw.origin,
		status: raw.status,
		amount: num(raw.amount),
		merchantRaw: raw.merchant_raw,
		merchantClean: raw.merchant_clean ?? null,
		categoryId: raw.category_id ?? null,
		transactionDate: String(raw.transaction_date).slice(0, 10),
		userNotes: raw.user_notes ?? null,
		isCleared: Boolean(raw.is_cleared)
	};
}

export async function fetchAccounts(): Promise<Account[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts`, { headers: authHeaders() });
	if (!res.ok) throw new Error(`Failed to fetch accounts (${res.status})`);
	const data = await res.json();
	return Array.isArray(data) ? data.map(mapAccount) : [];
}

export async function createAccount(payload: {
	name: string;
	type: string;
	current_balance: number;
	is_on_budget?: boolean;
}): Promise<Account> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to create account (${res.status})`);
	}
	return mapAccount(await res.json());
}

export async function fetchSolvencyReport(): Promise<SolvencyReport> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts/solvency`, { headers: authHeaders() });
	if (!res.ok) throw new Error(`Failed to fetch solvency (${res.status})`);
	const raw = await res.json();
	return {
		totalLiquidCash: num(raw.total_liquid_cash),
		totalCreditCardDebt: num(raw.total_credit_card_debt),
		solvencyBuffer: num(raw.solvency_buffer),
		isFloatingDebt: Boolean(raw.is_floating_debt),
		floatingDebtAmount: num(raw.floating_debt_amount)
	};
}

export async function fetchTransactions(): Promise<TransactionView[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions`, { headers: authHeaders() });
	if (!res.ok) throw new Error(`Failed to fetch transactions (${res.status})`);
	const data = await res.json();
	return Array.isArray(data) ? data.map(mapTransaction) : [];
}

export async function createTransaction(payload: CreateTransactionPayload): Promise<TransactionView> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to create transaction (${res.status})`);
	}
	return mapTransaction(await res.json());
}
