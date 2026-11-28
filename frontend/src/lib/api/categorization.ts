import { getAppConfig } from '$lib/config';
import { buildAuthHeaders } from './headers';
import type {
	ApplySimilarPayload,
	CategorizationRule,
	CategorizationSuggestion,
	CreateRulePayload,
	UpdateRulePayload
} from '$lib/types/categorization';

function optNum(val: unknown): number | null {
	if (val === null || val === undefined || val === '') return null;
	const n = typeof val === 'number' ? val : Number(val);
	return Number.isFinite(n) ? n : null;
}

async function authHeaders(): Promise<HeadersInit> {
	return buildAuthHeaders();
}

function mapRule(raw: any): CategorizationRule {
	return {
		id: raw.id,
		workspaceId: raw.workspace_id,
		priority: Number(raw.priority ?? 0),
		matchType: raw.match_type ?? 'CONTAINS',
		merchantPattern: raw.merchant_pattern ?? '',
		accountId: raw.account_id ?? null,
		direction: raw.direction ?? 'ANY',
		minAmount: optNum(raw.min_amount),
		maxAmount: optNum(raw.max_amount),
		categoryId: raw.category_id ?? null,
		plannedIncomeName: raw.planned_income_name ?? null,
		renameMerchant: raw.rename_merchant ?? null,
		requireReview: Boolean(raw.require_review),
		isActive: raw.is_active !== false,
		createdAt: String(raw.created_at ?? ''),
		updatedAt: String(raw.updated_at ?? '')
	};
}

function mapSuggestion(raw: any): CategorizationSuggestion {
	return {
		targetType: raw.target_type ?? 'CATEGORY',
		categoryId: raw.category_id ?? null,
		plannedIncomeId: raw.planned_income_id ?? null,
		name: raw.name ?? '',
		groupName: raw.group_name ?? '',
		score: Number(raw.score ?? 0),
		reason: raw.reason ?? ''
	};
}

function serializeRuleBody(payload: CreateRulePayload | UpdateRulePayload, includeApplyNow = false): Record<string, unknown> {
	const body: Record<string, unknown> = {
		match_type: payload.matchType,
		merchant_pattern: payload.merchantPattern,
		account_id: payload.accountId ?? null,
		direction: payload.direction,
		min_amount: payload.minAmount ?? null,
		max_amount: payload.maxAmount ?? null,
		category_id: payload.categoryId ?? null,
		planned_income_name: payload.plannedIncomeName ?? null,
		rename_merchant: payload.renameMerchant ?? null,
		require_review: Boolean(payload.requireReview)
	};
	if (payload.isActive !== undefined) body.is_active = payload.isActive;
	if (includeApplyNow && 'applyNow' in payload && payload.applyNow !== undefined) {
		body.apply_now = payload.applyNow;
	}
	return body;
}

export async function fetchCategorizationSuggestions(
	month: string,
	transactionId?: string
): Promise<Record<string, CategorizationSuggestion[]>> {
	const { apiBaseUrl } = getAppConfig();
	const qs = new URLSearchParams({ month });
	if (transactionId) qs.set('transaction_id', transactionId);
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/suggestions?${qs.toString()}`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch suggestions (${res.status})`);
	}
	const data = await res.json();
	const rawMap = data?.suggestions ?? {};
	const out: Record<string, CategorizationSuggestion[]> = {};
	for (const [txId, items] of Object.entries(rawMap)) {
		if (Array.isArray(items) && items.length > 0) {
			out[txId] = items.map(mapSuggestion);
		}
	}
	return out;
}

export async function fetchCategorizationRules(): Promise<CategorizationRule[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/rules`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to fetch categorization rules (${res.status})`);
	}
	const data = await res.json();
	return Array.isArray(data?.rules) ? data.rules.map(mapRule) : [];
}

export async function createCategorizationRule(
	payload: CreateRulePayload
): Promise<{ rule: CategorizationRule; appliedCount: number }> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/rules`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify(serializeRuleBody(payload, true))
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to create rule (${res.status})`);
	}
	const data = await res.json();
	return {
		rule: mapRule(data.rule),
		appliedCount: Number(data.applied_count ?? 0)
	};
}

export async function updateCategorizationRule(
	id: string,
	payload: UpdateRulePayload
): Promise<CategorizationRule> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/rules/${encodeURIComponent(id)}`, {
		method: 'PUT',
		headers: await authHeaders(),
		body: JSON.stringify(serializeRuleBody(payload, false))
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to update rule (${res.status})`);
	}
	return mapRule(await res.json());
}

export async function deleteCategorizationRule(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/rules/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(`Failed to delete rule (${res.status})`);
	}
}

export async function reorderCategorizationRules(ruleIds: string[]): Promise<CategorizationRule[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/rules/reorder`, {
		method: 'PUT',
		headers: await authHeaders(),
		body: JSON.stringify({ rule_ids: ruleIds })
	});
	if (!res.ok) {
		throw new Error(`Failed to reorder rules (${res.status})`);
	}
	const data = await res.json();
	return Array.isArray(data?.rules) ? data.rules.map(mapRule) : [];
}

export async function applyCategorizationRule(id: string): Promise<number> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/rules/${encodeURIComponent(id)}/apply`, {
		method: 'POST',
		headers: await authHeaders()
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to apply rule (${res.status})`);
	}
	const data = await res.json();
	return Number(data?.updated_count ?? 0);
}

export async function applySimilarCategorization(payload: ApplySimilarPayload): Promise<number> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/categorization/apply-similar`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({
			merchant_pattern: payload.merchantPattern,
			match_type: payload.matchType ?? 'CONTAINS',
			direction: payload.direction ?? 'ANY',
			category_id: payload.categoryId ?? null,
			planned_income_id: payload.plannedIncomeId ?? null,
			only_uncategorized: payload.onlyUncategorized ?? true
		})
	});
	if (!res.ok) {
		const errBody = await res.json().catch(() => ({}));
		throw new Error(errBody.error || `Failed to apply to similar transactions (${res.status})`);
	}
	const data = await res.json();
	return Number(data?.updated_count ?? 0);
}
