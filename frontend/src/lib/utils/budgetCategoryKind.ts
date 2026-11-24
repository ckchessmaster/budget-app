const FIXED_GROUP_RE = /\b(fixed|bills|debt\s*paydown|housing)\b/i;
const FIXED_NAME_RE =
	/\b(rent|mortgage|utilit(y|ies)|electric|electricity|water|natural gas|gas bill|internet|wifi|phone|cell|mobile|insurance|hoa|property tax|childcare|daycare|tuition|loan|car payment|auto loan|student loan|subscription|streaming|netflix|spotify|gym|membership|paycheck|salary|bill|bills|housing)\b/i;

/**
 * Classifies a budget category as fixed (e.g. rent, utilities, debt payments)
 * vs variable spending (e.g. groceries, dining out) using group and category names.
 */
export function isFixedCategory(name: string, groupName?: string): boolean {
	if (groupName && FIXED_GROUP_RE.test(groupName)) return true;
	return FIXED_NAME_RE.test(name);
}
