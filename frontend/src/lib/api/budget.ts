import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import type {
	CategoryBudget,
	CreateCategoryPayload,
	CreateIncomePayload,
	MonthBudgetDetails,
	MonthSummary,
	PlannedIncome,
	ReallocatePayload
} from '$lib/types/budget';

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

function mapCategory(raw: any): CategoryBudget {
	return {
		id: raw.id,
		name: raw.name,
		groupName: raw.group_name,
		rolloverMode: raw.rollover_mode,
		sweepTargetCategoryId: raw.sweep_target_category_id ?? null,
		targetAmount: raw.target_amount != null ? num(raw.target_amount) : null,
		targetDate: raw.target_date ? String(raw.target_date).slice(0, 10) : null,
		targetCadence: raw.target_cadence ?? 'NONE',
		sortOrder: raw.sort_order ?? 0,
		originalPlanned: num(raw.original_planned),
		effectiveTarget: num(raw.effective_target),
		spentCleared: num(raw.spent_cleared),
		spentPending: num(raw.spent_pending),
		remaining: num(raw.remaining),
		pacing: raw.pacing
			? {
					monthlyDue: num(raw.pacing.monthly_due),
					monthsRemaining: raw.pacing.months_remaining,
					isMet: Boolean(raw.pacing.is_met)
				}
			: undefined
	};
}

function mapSummary(raw: any): MonthSummary {
	return {
		yearMonth: String(raw.year_month).slice(0, 10),
		totalIncomeExpected: num(raw.total_income_expected),
		totalAllocated: num(raw.total_allocated),
		readyToAssign: num(raw.ready_to_assign),
		totalSpentCleared: num(raw.total_spent_cleared),
		totalSpentPending: num(raw.total_spent_pending),
		isUnbalanced: Boolean(raw.is_unbalanced)
	};
}

function mapIncome(raw: any): PlannedIncome {
	return {
		id: raw.id,
		workspaceId: raw.workspace_id,
		yearMonth: String(raw.year_month).slice(0, 10),
		name: raw.name,
		amount: num(raw.amount),
		createdAt: raw.created_at
	};
}

export async function fetchMonthBudget(month: string): Promise<MonthBudgetDetails> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/budget?month=${encodeURIComponent(month)}`, {
		headers: authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch budget (${res.status})`);
	}
	const data = await res.json();
	return {
		summary: mapSummary(data.summary),
		categories: Array.isArray(data.categories) ? data.categories.map(mapCategory) : [],
		incomes: Array.isArray(data.incomes) ? data.incomes.map(mapIncome) : []
	};
}

export async function createCategory(payload: CreateCategoryPayload): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categories`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to create category (${res.status})`);
	}
}

export async function updateCategory(id: string, payload: CreateCategoryPayload): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categories/${id}`, {
		method: 'PUT',
		headers: authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to update category (${res.status})`);
	}
}

export async function deleteCategory(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categories/${id}`, {
		method: 'DELETE',
		headers: authHeaders()
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to delete category (${res.status})`);
	}
}

export async function reallocateFunds(payload: ReallocatePayload): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/budget/reallocate`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to reallocate funds (${res.status})`);
	}
}

export async function createPlannedIncome(payload: CreateIncomePayload): Promise<PlannedIncome> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/budget/incomes`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to create planned income (${res.status})`);
	}
	return mapIncome(await res.json());
}

export async function deletePlannedIncome(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/budget/incomes/${id}`, {
		method: 'DELETE',
		headers: authHeaders()
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to delete planned income (${res.status})`);
	}
}
