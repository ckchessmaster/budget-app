<script module lang="ts">
	export const FOCUS_ALL = 'ALL';
	export const FOCUS_INCOME = 'INCOME';
</script>

<script lang="ts">
	interface Props {
		groups: string[];
		selected: string;
		onSelect: (value: string) => void;
	}

	let { groups, selected, onSelect }: Props = $props();

	let pills = $derived.by(() => {
		const seen = new Set<string>();
		const uniqueGroups: string[] = [];
		for (const g of groups) {
			const trimmed = g.trim();
			if (!trimmed || seen.has(trimmed)) continue;
			seen.add(trimmed);
			uniqueGroups.push(trimmed);
		}
		return [
			{ id: FOCUS_ALL, label: 'All Categories' },
			{ id: FOCUS_INCOME, label: 'Income' },
			...uniqueGroups.map((g) => ({ id: g, label: g }))
		];
	});
</script>

<nav
	aria-label="Category focus"
	data-testid="category-focus-pills"
	class="flex flex-wrap items-center gap-1.5 sm:gap-2"
>
	{#each pills as pill (pill.id)}
		<button
			type="button"
			data-testid="focus-pill"
			data-pill-id={pill.id}
			aria-pressed={selected === pill.id}
			onclick={() => onSelect(pill.id)}
			class="shrink-0 whitespace-nowrap rounded-full border px-3.5 py-1.5 text-xs font-semibold transition-colors {selected === pill.id
				? 'bg-accent-pop text-accent-primary-text border-accent-pop'
				: 'bg-surface text-ink-secondary border-border hover:bg-surface-hover hover:text-ink-primary'}"
		>
			{pill.label}
		</button>
	{/each}
</nav>
