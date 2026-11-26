import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import type {
	AccountItem,
	CreateAccountPayload,
	CreateTransactionPayload,
	ReplaceSplitsPayload,
	SolvencyReport,
	TransactionItemModel,
	UpdateAccountPayload,
	UpdateTransactionPayload
} from '$lib/types/transaction';

function num(val: unknown): number {
	if (typeof val === 'number') return val;
	if (typeof val === 'string') {
		const parsed = Number(val);
		return Number.isFinite(parsed) ? parsed : 0;
	}
	return 0;
}

async function authHeaders(): Promise<HeadersInit> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	const token = await auth.getValidToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
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
		plannedIncomeId: raw.planned_income_id ?? null,
		transactionDate: String(raw.transaction_date).slice(0, 10),
		budgetMonth: raw.budget_month ? String(raw.budget_month).slice(0, 7) : null,
		userNotes: raw.user_notes ?? null,
		isCleared: Boolean(raw.is_cleared),
		isTransfer: Boolean(raw.is_transfer),
		categorizedBy: raw.categorized_by === 'RULE' ? 'RULE' : 'MANUAL',
		appliedRuleId: raw.applied_rule_id ?? null,
		needsReview: Boolean(raw.needs_review),
		reviewIgnored: Boolean(raw.review_ignored),
		settledDelta: raw.settled_delta != null ? num(raw.settled_delta) : null,
		splits: Array.isArray(raw.splits)
			? raw.splits.map((s: any) => ({
					id: s.id,
					amount: num(s.amount),
					categoryId: s.category_id ?? null,
					plannedIncomeId: s.planned_income_id ?? null,
					userNotes: s.user_notes ?? null
				}))
			: undefined
	};
}

export async function fetchTransactions(params?: {
	accountId?: string;
	categoryId?: string;
	status?: string;
	month?: string;
	toReview?: boolean;
	page?: number;
	limit?: number;
}): Promise<{ items: TransactionItemModel[]; total: number }> {
	const { apiBaseUrl } = getAppConfig();
	const qs = new URLSearchParams();
	if (params?.accountId) qs.set('account_id', params.accountId);
	if (params?.categoryId) qs.set('category_id', params.categoryId);
	if (params?.status) qs.set('status', params.status);
	if (params?.month) qs.set('month', params.month);
	if (params?.toReview) qs.set('to_review', 'true');
	if (params?.page) qs.set('page', String(params.page));
	if (params?.limit) qs.set('limit', String(params.limit));

	const query = qs.toString();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions${query ? `?${query}` : ''}`, {
		headers: await authHeaders()
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
		headers: await authHeaders(),
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

export async function updateTransaction(
	id: string,
	payload: UpdateTransactionPayload
): Promise<TransactionItemModel> {
	const { apiBaseUrl } = getAppConfig();
	const body: Record<string, unknown> = {};
	if (payload.clearCategory) {
		body.clear_category = true;
	} else if (payload.categoryId !== undefined) {
		body.category_id = payload.categoryId;
	}
	if (payload.clearPlannedIncome) {
		body.clear_planned_income = true;
	} else if (payload.plannedIncomeId !== undefined) {
		body.planned_income_id = payload.plannedIncomeId;
	}
	if (payload.merchantClean !== undefined) body.merchant_clean = payload.merchantClean;
	if (payload.userNotes !== undefined) body.user_notes = payload.userNotes;
	if (payload.status !== undefined) body.status = payload.status;
	if (payload.isCleared !== undefined) body.is_cleared = payload.isCleared;
	if (payload.amount !== undefined) body.amount = payload.amount;
	if (payload.approveReview !== undefined) body.approve_review = payload.approveReview;
	if (payload.clearBudgetMonth) {
		body.clear_budget_month = true;
	} else if (payload.budgetMonth !== undefined) {
		body.budget_month = payload.budgetMonth;
	}
	if (payload.reviewIgnored !== undefined) body.review_ignored = payload.reviewIgnored;

	const res = await fetch(`${apiBaseUrl}/api/v1/transactions/${encodeURIComponent(id)}`, {
		method: 'PATCH',
		headers: await authHeaders(),
		body: JSON.stringify(body)
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to update transaction (${res.status})`);
	}
	return mapTransaction(await res.json());
}

export async function replaceTransactionSplits(
	id: string,
	payload: ReplaceSplitsPayload
): Promise<TransactionItemModel> {
	const { apiBaseUrl } = getAppConfig();
	const body: Record<string, unknown> = {
		splits: payload.splits.map((s) => ({
			amount: s.amount,
			category_id: s.categoryId ?? null,
			planned_income_id: s.plannedIncomeId ?? null,
			user_notes: s.userNotes ?? ''
		}))
	};
	if (payload.categoryId !== undefined) body.category_id = payload.categoryId;
	if (payload.plannedIncomeId !== undefined) body.planned_income_id = payload.plannedIncomeId;

	const res = await fetch(`${apiBaseUrl}/api/v1/transactions/${encodeURIComponent(id)}/splits`, {
		method: 'PUT',
		headers: await authHeaders(),
		body: JSON.stringify(body)
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to update transaction splits (${res.status})`);
	}
	return mapTransaction(await res.json());
}

export async function ignoreReviewBefore(beforeMonth: string): Promise<{ updated: number }> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions/ignore-review-before`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({ before_month: beforeMonth })
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to skip older reviews (${res.status})`);
	}
	return res.json();
}

export async function deleteTransaction(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/transactions/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to delete transaction (${res.status})`);
	}
}

function mapAccount(a: any): AccountItem {
	return {
		id: a.id,
		name: a.name,
		type: a.type,
		currentBalance: num(a.current_balance),
		isOnBudget: Boolean(a.is_on_budget),
		cardMode: a.card_mode ?? 'PAY_IN_FULL',
		aprPercentage: num(a.apr_percentage),
		externalAccountId: a.external_account_id ?? null,
		initialDebtBaseline: num(a.initial_debt_baseline),
		statementDueDay: a.statement_due_day ?? null,
		debtPayoffCategoryId: a.debt_payoff_category_id ?? null
	};
}

export async function fetchAccounts(): Promise<AccountItem[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch accounts (${res.status})`);
	}
	const raw = await res.json();
	if (!Array.isArray(raw)) return [];
	return raw.map(mapAccount);
}

export async function createAccount(payload: CreateAccountPayload): Promise<AccountItem> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({
			name: payload.name,
			type: payload.type,
			current_balance: payload.currentBalance,
			is_on_budget: payload.isOnBudget,
			card_mode: payload.cardMode ?? 'PAY_IN_FULL',
			initial_debt_baseline: payload.initialDebtBaseline ?? 0,
			apr_percentage: payload.aprPercentage ?? 0,
			statement_due_day: payload.statementDueDay ?? null,
			debt_payoff_category_id: payload.debtPayoffCategoryId ?? null
		})
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to create account (${res.status})`);
	}
	return mapAccount(await res.json());
}

export async function updateAccount(id: string, payload: UpdateAccountPayload): Promise<AccountItem> {
	const { apiBaseUrl } = getAppConfig();
	const body: Record<string, unknown> = {};
	if (payload.name !== undefined) body.name = payload.name;
	if (payload.type !== undefined) body.type = payload.type;
	if (payload.currentBalance !== undefined) body.current_balance = payload.currentBalance;
	if (payload.isOnBudget !== undefined) body.is_on_budget = payload.isOnBudget;
	if (payload.cardMode !== undefined) body.card_mode = payload.cardMode;
	if (payload.initialDebtBaseline !== undefined) body.initial_debt_baseline = payload.initialDebtBaseline;
	if (payload.aprPercentage !== undefined) body.apr_percentage = payload.aprPercentage;
	if (payload.statementDueDay !== undefined) body.statement_due_day = payload.statementDueDay;
	if (payload.clearStatementDueDay !== undefined) body.clear_statement_due_day = payload.clearStatementDueDay;
	if (payload.debtPayoffCategoryId !== undefined) body.debt_payoff_category_id = payload.debtPayoffCategoryId;
	if (payload.clearDebtPayoffCategory !== undefined) body.clear_debt_payoff_category = payload.clearDebtPayoffCategory;
	if (payload.unlinkExternal !== undefined) body.unlink_external = payload.unlinkExternal;
	if (payload.recordBalanceAdjustment !== undefined) body.record_balance_adjustment = payload.recordBalanceAdjustment;

	const res = await fetch(`${apiBaseUrl}/api/v1/accounts/${encodeURIComponent(id)}`, {
		method: 'PATCH',
		headers: await authHeaders(),
		body: JSON.stringify(body)
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to update account (${res.status})`);
	}
	return mapAccount(await res.json());
}

export async function deleteAccount(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: await authHeaders()
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to delete account (${res.status})`);
	}
}

export async function fetchSolvencyReport(): Promise<SolvencyReport> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/accounts/solvency`, {
		headers: await authHeaders()
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
		floatingDebtAmount: num(raw.floating_debt_amount),
		totalReservedInFunds: num(raw.total_reserved_in_funds),
		unreservedCash: raw.unreserved_cash != null ? num(raw.unreserved_cash) : num(raw.solvency_buffer),
		accountReservations: Array.isArray(raw.account_reservations)
			? raw.account_reservations.map((ar: any) => ({
					accountId: ar.account_id,
					reservedTotal: num(ar.reserved_total),
					funds: Array.isArray(ar.funds)
						? ar.funds.map((f: any) => ({
								categoryId: f.category_id,
								name: f.name,
								balance: num(f.balance)
							}))
						: []
				}))
			: []
	};
}
