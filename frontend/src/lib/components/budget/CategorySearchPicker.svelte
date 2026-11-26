<script lang="ts">
	export interface CategoryPickerOption {
		id: string;
		name: string;
		groupName?: string;
		subtitle?: string;
	}

	interface Props {
		categories: CategoryPickerOption[];
		incomes?: Array<{ id: string; name: string }>;
		value?: string;
		id?: string;
		ariaLabel?: string;
		emptyOptionLabel?: string;
		placeholder?: string;
		fallbackLabel?: string;
		collapsible?: boolean;
		disabled?: boolean;
		maxHeightClass?: string;
		testIdPrefix?: string;
	}

	let {
		categories,
		incomes = [],
		value = $bindable(''),
		id,
		ariaLabel = 'Select category',
		emptyOptionLabel,
		placeholder = 'Search categories...',
		fallbackLabel = 'Select category...',
		collapsible = false,
		disabled = false,
		maxHeightClass = 'max-h-44',
		testIdPrefix = 'cat-search-picker'
	}: Props = $props();

	let isOpen = $state(false);
	let query = $state('');
	let searchEl = $state<HTMLInputElement | null>(null);

	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryPickerOption[]>();
		for (const cat of categories) {
			const grp = cat.groupName?.trim() || 'Categories';
			const list = groups.get(grp) ?? [];
			list.push(cat);
			groups.set(grp, list);
		}
		return Array.from(groups.entries());
	});

	let filteredGroups = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return groupedCategories;
		return groupedCategories
			.map(
				([groupName, cats]) =>
					[
						groupName,
						cats.filter(
							(c) =>
								c.name.toLowerCase().includes(q) ||
								groupName.toLowerCase().includes(q) ||
								(c.subtitle ?? '').toLowerCase().includes(q)
						)
					] as [string, CategoryPickerOption[]]
			)
			.filter(([, cats]) => cats.length > 0);
	});

	let filteredIncomes = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return incomes;
		return incomes.filter((inc) => inc.name.toLowerCase().includes(q) || 'income'.includes(q));
	});

	let selectedLabel = $derived.by(() => {
		if (!value) return emptyOptionLabel ?? fallbackLabel;
		if (value.startsWith('income:')) {
			const incId = value.slice('income:'.length);
			const inc = incomes.find((i) => i.id === incId);
			return inc ? `Income: ${inc.name}` : 'Income';
		}
		const cat = categories.find((c) => c.id === value);
		if (!cat) return emptyOptionLabel ?? fallbackLabel;
		return cat.subtitle ? `${cat.name} (${cat.subtitle})` : cat.name;
	});

	function selectValue(next: string) {
		value = next;
		if (collapsible) {
			isOpen = false;
			query = '';
		}
	}

	function toggleOpen() {
		if (disabled) return;
		isOpen = !isOpen;
		if (isOpen) {
			query = '';
			queueMicrotask(() => searchEl?.focus());
		}
	}
</script>

{#if collapsible}
	<div class="space-y-1.5">
		<button
			{id}
			type="button"
			aria-label={ariaLabel}
			aria-expanded={isOpen}
			data-testid={`${testIdPrefix}-trigger`}
			{disabled}
			onclick={toggleOpen}
			class="w-full flex items-center justify-between gap-2 rounded-lg border border-border bg-canvas px-2.5 py-1.5 text-left text-xs text-ink-primary hover:bg-surface-hover disabled:opacity-50 focus:border-accent-pop focus:outline-none transition-colors"
		>
			<span class="truncate {!value ? 'text-ink-secondary' : 'font-medium'}">{selectedLabel}</span>
			<svg
				class="w-3.5 h-3.5 shrink-0 text-ink-muted transition-transform {isOpen ? 'rotate-180' : ''}"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				aria-hidden="true"
			>
				<path d="m6 9 6 6 6-6" />
			</svg>
		</button>

		{#if isOpen && !disabled}
			<div
				data-testid={`${testIdPrefix}-panel`}
				class="rounded-lg border border-border bg-canvas p-2 space-y-2 shadow-sm"
			>
				<input
					bind:this={searchEl}
					type="search"
					aria-label={placeholder}
					{placeholder}
					bind:value={query}
					class="w-full rounded-md border border-border bg-surface px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
				/>

				<div
					data-testid={`${testIdPrefix}-list`}
					class="{maxHeightClass} overflow-y-auto overscroll-contain-scroll space-y-2"
				>
					{#if emptyOptionLabel}
						<div class="pb-1.5 border-b border-border">
							<button
								type="button"
								data-testid={`${testIdPrefix}-empty`}
								onclick={() => selectValue('')}
								class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {value === ''
									? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
									: 'border-border bg-surface text-ink-secondary hover:text-ink-primary hover:bg-surface-hover'}"
							>
								{emptyOptionLabel}
							</button>
						</div>
					{/if}

					{#if filteredGroups.length === 0 && filteredIncomes.length === 0}
						<p class="text-[11px] text-ink-muted py-1 text-center">No matching categories</p>
					{:else}
						{#if filteredIncomes.length > 0}
							<div class="space-y-1">
								<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
									Income
								</p>
								<div class="flex flex-wrap gap-1.5">
									{#each filteredIncomes as inc (inc.id)}
										{@const val = `income:${inc.id}`}
										<button
											type="button"
											data-testid={`${testIdPrefix}-option`}
											onclick={() => selectValue(val)}
											class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {value === val
												? 'border-status-good bg-status-good-bg text-status-good font-semibold'
												: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
										>
											{inc.name}
										</button>
									{/each}
								</div>
							</div>
						{/if}

						{#each filteredGroups as [grp, cats] (grp)}
							<div class="space-y-1">
								<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
									{grp}
								</p>
								<div class="flex flex-wrap gap-1.5">
									{#each cats as cat (cat.id)}
										<button
											type="button"
											data-testid={`${testIdPrefix}-option`}
											onclick={() => selectValue(cat.id)}
											class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {value === cat.id
												? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
												: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
										>
											{cat.name}{cat.subtitle ? ` (${cat.subtitle})` : ''}
										</button>
									{/each}
								</div>
							</div>
						{/each}
					{/if}
				</div>
			</div>
		{/if}
	</div>
{:else}
	<div class="space-y-1.5">
		<input
			{id}
			type="search"
			aria-label={placeholder}
			{placeholder}
			bind:value={query}
			{disabled}
			class="w-full rounded-lg border border-border bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent-pop focus:outline-none"
		/>

		<div
			data-testid={`${testIdPrefix}-list`}
			class="{maxHeightClass} overflow-y-auto overscroll-contain-scroll rounded-lg border border-border bg-canvas p-2.5 space-y-2"
		>
			{#if emptyOptionLabel}
				<div class="pb-1.5 border-b border-border">
					<button
						type="button"
						data-testid={`${testIdPrefix}-empty`}
						onclick={() => selectValue('')}
						class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {value === ''
							? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
							: 'border-border bg-surface text-ink-secondary hover:text-ink-primary hover:bg-surface-hover'}"
					>
						{emptyOptionLabel}
					</button>
				</div>
			{/if}

			{#if filteredGroups.length === 0 && filteredIncomes.length === 0}
				<p class="text-[11px] text-ink-muted py-1 text-center">No matching categories</p>
			{:else}
				{#each filteredGroups as [grp, cats] (grp)}
					<div class="space-y-1">
						<p class="text-[10px] font-semibold uppercase tracking-wider text-ink-muted">
							{grp}
						</p>
						<div class="flex flex-wrap gap-1.5">
							{#each cats as cat (cat.id)}
								<button
									type="button"
									data-testid={`${testIdPrefix}-option`}
									onclick={() => selectValue(cat.id)}
									class="rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors {value === cat.id
										? 'border-accent-pop bg-accent-tint text-accent-pop font-semibold'
										: 'border-border bg-surface text-ink-primary hover:bg-surface-hover'}"
								>
									{cat.name}{cat.subtitle ? ` (${cat.subtitle})` : ''}
								</button>
							{/each}
						</div>
					</div>
				{/each}
			{/if}
		</div>
	</div>
{/if}
