export interface GroupColor {
	name: string;
	stroke: string;
	dotClass: string;
	textClass: string;
	ringClass: string;
}

export const GROUP_COLOR_PALETTE: GroupColor[] = [
	{
		name: 'Sky',
		stroke: '#0284c7',
		dotClass: 'bg-sky-500',
		textClass: 'text-sky-600 dark:text-sky-400',
		ringClass: 'ring-sky-500/40'
	},
	{
		name: 'Amber',
		stroke: '#f59e0b',
		dotClass: 'bg-amber-500',
		textClass: 'text-amber-600 dark:text-amber-400',
		ringClass: 'ring-amber-500/40'
	},
	{
		name: 'Emerald',
		stroke: '#10b981',
		dotClass: 'bg-emerald-500',
		textClass: 'text-emerald-600 dark:text-emerald-400',
		ringClass: 'ring-emerald-500/40'
	},
	{
		name: 'Violet',
		stroke: '#8b5cf6',
		dotClass: 'bg-violet-500',
		textClass: 'text-violet-600 dark:text-violet-400',
		ringClass: 'ring-violet-500/40'
	},
	{
		name: 'Rose',
		stroke: '#f43f5e',
		dotClass: 'bg-rose-500',
		textClass: 'text-rose-600 dark:text-rose-400',
		ringClass: 'ring-rose-500/40'
	},
	{
		name: 'Teal',
		stroke: '#14b8a6',
		dotClass: 'bg-teal-500',
		textClass: 'text-teal-600 dark:text-teal-400',
		ringClass: 'ring-teal-500/40'
	},
	{
		name: 'Indigo',
		stroke: '#6366f1',
		dotClass: 'bg-indigo-500',
		textClass: 'text-indigo-600 dark:text-indigo-400',
		ringClass: 'ring-indigo-500/40'
	},
	{
		name: 'Orange',
		stroke: '#f97316',
		dotClass: 'bg-orange-500',
		textClass: 'text-orange-600 dark:text-orange-400',
		ringClass: 'ring-orange-500/40'
	}
];

const PALETTE = GROUP_COLOR_PALETTE;

function hashString(input: string): number {
	let hash = 0;
	for (let i = 0; i < input.length; i++) {
		hash = (hash * 31 + input.charCodeAt(i)) >>> 0;
	}
	return hash;
}

export function getGroupColor(groupName: string, index?: number): GroupColor {
	const idx = typeof index === 'number' && index >= 0 ? index : hashString(groupName);
	return PALETTE[idx % PALETTE.length];
}

export function groupSlug(groupName: string): string {
	return (
		'group-' +
		groupName
			.toLowerCase()
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-|-$/g, '')
	);
}
