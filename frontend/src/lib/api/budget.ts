import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import type {
	CategoryBudget,
	CreateCategoryPayload,
	CreateIncomePayload,
	MonthBudgetDetails,
	MonthSummary,
	PlannedIncome,
	ReallocatePayload,
	SpendRecommendation
} from '$lib/types/budget';

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
		headers: await authHeaders()
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

export async function fetchCategories(): Promise<Array<{ id: string; name: string; groupName: string }>> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categories`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch categories (${res.status})`);
	}
	const raw = await res.json();
	if (!Array.isArray(raw)) return [];
	return raw.map((c: any) => ({
		id: c.id,
		name: c.name,
		groupName: c.group_name
	}));
}

export async function createCategory(payload: CreateCategoryPayload): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categories`, {
		method: 'POST',
		headers: await authHeaders(),
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
		headers: await authHeaders(),
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
		headers: await authHeaders()
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
		headers: await authHeaders(),
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
		headers: await authHeaders(),
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
		headers: await authHeaders()
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to delete planned income (${res.status})`);
	}
}

export async function updateMonthlyPlanned(
	categoryId: string,
	yearMonth: string,
	originalPlanned: number
): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/budget/categories/${encodeURIComponent(categoryId)}`, {
		method: 'PUT',
		headers: await authHeaders(),
		body: JSON.stringify({
			year_month: yearMonth,
			original_planned: originalPlanned
		})
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to update monthly planned budget (${res.status})`);
	}
}

function mapSpendRecommendation(raw: any): SpendRecommendation {
	return {
		categoryId: raw.category_id,
		categoryName: raw.category_name,
		currentPlanned: num(raw.current_planned),
		trailingMedianSpend: num(raw.trailing_median_spend),
		percentageDeviation: num(raw.percentage_deviation),
		shouldPrompt: Boolean(raw.should_prompt)
	};
}

export async function fetchSpendRecommendations(month: string): Promise<SpendRecommendation[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(
		`${apiBaseUrl}/api/v1/budget/recommendations?month=${encodeURIComponent(month)}`,
		{
			headers: await authHeaders()
		}
	);
	if (!res.ok) {
		throw new Error(`Failed to fetch spend recommendations (${res.status})`);
	}
	const raw = await res.json();
	if (!Array.isArray(raw)) return [];
	return raw.map(mapSpendRecommendation);
}
