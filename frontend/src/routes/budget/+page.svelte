<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import {
		deleteCategory,
		fetchMonthBudget,
		fetchSpendRecommendations,
		updateCategory,
		updateMonthlyPlanned
	} from '$lib/api/budget';
	import {
		applySimilarCategorization,
		fetchCategorizationSuggestions
	} from '$lib/api/categorization';
	import {
		createTransaction,
		deleteTransaction,
		fetchAccounts,
		fetchSolvencyReport,
		fetchTransactions,
		ignoreReviewBefore,
		replaceTransactionSplits,
		updateTransaction
	} from '$lib/api/transactions';
	import RuleModal from '$lib/components/settings/RuleModal.svelte';
	import BudgetHeader from '$lib/components/budget/BudgetHeader.svelte';
	import CategoryFocusPills, {
		FOCUS_ALL,
		FOCUS_INCOME
	} from '$lib/components/budget/CategoryFocusPills.svelte';
	import CategoryGroupCard from '$lib/components/budget/CategoryGroupCard.svelte';
	import CategoryGroupModal from '$lib/components/budget/CategoryGroupModal.svelte';
	import CategoryModal from '$lib/components/budget/CategoryModal.svelte';
	import CatchUpModal, { type CatchUpMode } from '$lib/components/budget/CatchUpModal.svelte';
	import CoverDrawer from '$lib/components/budget/CoverDrawer.svelte';
	import IncomeCard from '$lib/components/budget/IncomeCard.svelte';
	import MoveMonthConfirmModal from '$lib/components/budget/MoveMonthConfirmModal.svelte';
	import RecommendationBanner from '$lib/components/budget/RecommendationBanner.svelte';
	import TransactionDetailCard from '$lib/components/budget/TransactionDetailCard.svelte';
	import TransactionsDrawer from '$lib/components/budget/TransactionsDrawer.svelte';
	import TransactionsPanel from '$lib/components/budget/TransactionsPanel.svelte';
	import { BudgetLocalDB, type OfflineTransaction } from '$lib/offline/db';
	import {
		flushOfflineQueue,
		registerOnlineSyncListener,
		requestBackgroundSync
	} from '$lib/offline/sync';
	import { auth } from '$lib/stores/auth.svelte';
	import { demo } from '$lib/stores/demo.svelte';
	import type {
		CategoryBudget,
		CreateCategoryPayload,
		InlineActivityItem,
		MonthBudgetDetails,
		SpendRecommendation
	} from '$lib/types/budget';
	import type {
		ApplySimilarPayload,
		CategorizationSuggestion,
		CreateRulePayload
	} from '$lib/types/categorization';
	import type {
		AccountItem,
		ReplaceSplitsPayload,
		SolvencyReport,
		TransactionItemModel,
		UpdateTransactionPayload
	} from '$lib/types/transaction';
	import { GROUP_COLOR_PALETTE, getGroupColor } from '$lib/utils/budgetColors';
	import {
		buildMoveToMonthPatch,
		effectiveBudgetMonth,
		isCatchUpTransaction,
		isCategorizableTransaction,
		isMoveMonthPromptSkippedToday,
		isToReviewTransaction,
		mapOfflineQueuedToTransactions,
		skipMoveMonthPromptToday
	} from '$lib/utils/transactionReview';

	function defaultMonth(): string {
		const now = new Date();
		return `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`;
	}

	let currentMonth = $derived.by(() => {
		const q = page.url.searchParams.get('month');
		if (q && /^\d{4}-\d{2}$/.test(q)) return q;
		return defaultMonth();
	});

	let localDB = new BudgetLocalDB();
	let loadSeq = 0;
	let hasAutoFlushedOnBoot = false;

	let budgetData = $state<MonthBudgetDetails | null>(null);
	let transactions = $state<TransactionItemModel[]>([]);
	let toReviewTransactions = $state<TransactionItemModel[]>([]);
	let queuedTransactions = $state<OfflineTransaction[]>([]);
	let accounts = $state<AccountItem[]>([]);
	let solvency = $state<SolvencyReport | null>(null);
	let recommendations = $state<SpendRecommendation[]>([]);
	let suggestionsByTxId = $state<Record<string, CategorizationSuggestion[]>>({});

	let isRuleModalOpen = $state(false);
	let ruleModalPrefill = $state<Partial<CreateRulePayload> | null>(null);

	let isLoading = $state(false);
	let syncing = $state(false);
	let errorMsg = $state('');
	let isOffline = $state(typeof navigator !== 'undefined' ? !navigator.onLine : false);

	// Layout & panel state
	let isMobileDrawerOpen = $state(false);
	let lastUrlPanel = $state<string | null>(null);

	let displayMode = $state<'remaining' | 'spent'>('remaining');
	let activeCategoryFilterId = $state<string | null>(null);
	let activeFocusGroup = $state<string>(FOCUS_ALL);

	// Modals & Drawers
	let coverCategoryId = $state<string | null>(null);
	let isCategoryModalOpen = $state(false);
	let editingCategory = $state<CategoryBudget | null>(null);
	let modalDefaultGroup = $state('Essentials');
	let isGroupModalOpen = $state(false);
	let editingGroupName = $state<string | null>(null);
	let editingGroupColorIndex = $state(0);
	let draggingCategoryId = $state<string | null>(null);
	let draggingGroupName = $state<string | null>(null);
	let draggingTxId = $state<string | null>(null);
	let organizeMode = $state(false);
	let collapsedGroups = $state<Record<string, boolean>>({});
	let reorderAnnouncement = $state('');
	let announceTimer: ReturnType<typeof setTimeout> | null = null;
	let selectedTransactionId = $state<string | null>(null);
	let detailOpenSplitMode = $state(false);
	let isDesktop = $state(
		typeof window !== 'undefined' ? window.matchMedia('(min-width: 1024px)').matches : true
	);
	let mobileDetailDialogEl = $state<HTMLDialogElement | null>(null);
	let isCatchUpModalOpen = $state(false);
	let catchUpModalMode = $state<CatchUpMode>('intro');
	let hasAutoPromptedCatchUp = $state(false);
	let pendingMoveDrop = $state<{
		tx: TransactionItemModel;
		fromMonth: string;
		targetType: 'category' | 'income';
		targetId: string;
		targetLabel: string;
	} | null>(null);
	let isSubmittingMoveDrop = $state(false);

	$effect(() => {
		const panelParam = page.url.searchParams.get('panel');
		if (panelParam !== lastUrlPanel) {
			lastUrlPanel = panelParam;
			if (panelParam === 'transactions') {
				isMobileDrawerOpen = true;
			}
		}
	});

	function isNetworkError(err: unknown): boolean {
		return (typeof navigator !== 'undefined' && !navigator.onLine) || err instanceof TypeError;
	}

	async function loadFromOfflineCache(month: string, seq: number) {
		isOffline = true;
		recommendations = [];
		suggestionsByTxId = {};
		const [cachedAccts, cachedCats, cachedSnapshot] = await Promise.all([
			localDB.getCachedAccounts(),
			localDB.getCachedCategories(),
			localDB.getCachedBudgetSnapshot(month).catch(() => undefined)
		]);
		if (seq !== loadSeq) return;

		accounts = cachedAccts.map((a) => ({
			id: a.id,
			name: a.name,
			type: a.type as AccountItem['type'],
			currentBalance: a.currentBalance,
			isOnBudget: a.isOnBudget,
			cardMode: (a.cardMode as AccountItem['cardMode']) ?? 'PAY_IN_FULL',
			aprPercentage: 0,
			externalAccountId: null,
			initialDebtBaseline: 0,
			statementDueDay: null,
			debtPayoffCategoryId: null
		}));

		if (!budgetData && (cachedCats.length > 0 || cachedSnapshot)) {
			const sortedCats = [...cachedCats].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0));
			budgetData = {
				summary: cachedSnapshot?.summary ?? {
					yearMonth: `${month}-01`,
					totalIncomeExpected: 0,
					totalAllocated: 0,
					readyToAssign: 0,
					totalSpentCleared: 0,
					totalSpentPending: 0,
					isUnbalanced: false
				},
				categories: sortedCats.map((c, idx) => ({
					id: c.id,
					name: c.name,
					groupName: c.groupName,
					rolloverMode: 'SWEEP',
					sweepTargetCategoryId: null,
					targetAmount: null,
					targetDate: null,
					targetCadence: 'NONE',
					sortOrder: c.sortOrder ?? idx,
					originalPlanned: c.originalPlanned ?? 0,
					effectiveTarget: c.effectiveTarget ?? 0,
					spentCleared: c.spentCleared ?? 0,
					spentPending: c.spentPending ?? 0,
					remaining: c.remaining
				})),
				incomes: cachedSnapshot?.incomes ?? []
			};
		}
	}

	async function loadWorkspace(month: string, silent = false) {
		const seq = ++loadSeq;
		await auth.ready();
		if (!auth.isAuthenticated && !auth.canAccessOffline) {
			void goto('/login');
			return;
		}
		if (!silent) {
			isLoading = true;
		}
		errorMsg = '';

		try {
			const workspaceId = demo.workspaceId || undefined;
			let pending = await localDB.getPendingQueue(workspaceId);
			if (seq !== loadSeq) return;
			queuedTransactions = pending;

			if ((typeof navigator !== 'undefined' && !navigator.onLine) || !auth.isAuthenticated) {
				await loadFromOfflineCache(month, seq);
				return;
			}

			isOffline = false;
			if (!hasAutoFlushedOnBoot && pending.length > 0) {
				hasAutoFlushedOnBoot = true;
				await flushOfflineQueue(localDB, undefined, undefined, { workspaceId }).catch(() => null);
				pending = await localDB.getPendingQueue(workspaceId);
				if (seq !== loadSeq) return;
				queuedTransactions = pending;
			}

			const [budgetRes, txRes, toReviewRes, acctsRes, solvRes, recsRes, sugRes] = await Promise.all([
				fetchMonthBudget(month),
				fetchTransactions({ month, limit: 200 }).catch(() => ({ items: [], total: 0 })),
				fetchTransactions({ toReview: true, limit: 200 }).catch(() => ({ items: [], total: 0 })),
				fetchAccounts().catch(() => []),
				fetchSolvencyReport().catch(() => null),
				fetchSpendRecommendations(month).catch(() => []),
				fetchCategorizationSuggestions(month).catch(() => ({}))
			]);
			if (seq !== loadSeq) return;

			budgetData = budgetRes;
			transactions = txRes.items;
			toReviewTransactions = toReviewRes.items;
			accounts = acctsRes;
			solvency = solvRes;
			recommendations = recsRes;
			suggestionsByTxId = sugRes;

			if (!hasAutoPromptedCatchUp && !silent) {
				hasAutoPromptedCatchUp = true;
				const loadedAccountById = new Map(acctsRes.map((a) => [a.id, a]));
				if (txRes.items.some((tx) => isCatchUpTransaction(tx, loadedAccountById))) {
					catchUpModalMode = 'intro';
					isCatchUpModalOpen = true;
				}
			}

			await Promise.all([
				localDB.cacheAccounts(
					acctsRes.map((a) => ({
						id: a.id,
						name: a.name,
						type: a.type,
						currentBalance: a.currentBalance,
						isOnBudget: a.isOnBudget,
						cardMode: a.cardMode
					}))
				),
				localDB.cacheCategories(
					budgetRes.categories.map((c) => ({
						id: c.id,
						name: c.name,
						groupName: c.groupName,
						remaining: c.remaining,
						originalPlanned: c.originalPlanned,
						effectiveTarget: c.effectiveTarget,
						spentCleared: c.spentCleared,
						spentPending: c.spentPending,
						sortOrder: c.sortOrder
					}))
				),
				localDB.cacheBudgetSnapshot({
					yearMonth: month,
					summary: budgetRes.summary,
					incomes: budgetRes.incomes,
					updatedAt: Date.now()
				})
			]);
		} catch (err: any) {
			if (seq !== loadSeq) return;
			if (isNetworkError(err)) {
				await loadFromOfflineCache(month, seq);
				return;
			}
			errorMsg = err?.message || 'Could not load budget workspace.';
		} finally {
			if (seq === loadSeq) {
				isLoading = false;
			}
		}
	}

	$effect(() => {
		const version = demo.dataVersion;
		const authed = auth.isAuthenticated;
		if (authed || auth.canAccessOffline) {
			void loadWorkspace(currentMonth, version > 0);
		}
	});

	onMount(() => {
		const stopListening = registerOnlineSyncListener(() => {
			void loadWorkspace(currentMonth, true);
		});
		const onOnline = () => {
			isOffline = false;
			void (async () => {
				if (!auth.isAuthenticated && auth.hasCachedSession) {
					await auth.tryRestoreSession();
				}
				if (auth.isAuthenticated) {
					await flushOfflineQueue(localDB, undefined, undefined, {
						workspaceId: demo.workspaceId || undefined
					}).catch(() => null);
					await loadWorkspace(currentMonth, true);
				}
			})();
		};
		const onOffline = () => {
			isOffline = true;
		};
		window.addEventListener('online', onOnline);
		window.addEventListener('offline', onOffline);

		const mql = window.matchMedia('(min-width: 1024px)');
		isDesktop = mql.matches;
		const onMediaChange = (e: MediaQueryListEvent) => {
			isDesktop = e.matches;
		};
		mql.addEventListener('change', onMediaChange);

		let removeDialogLightDismiss: (() => void) | null = null;
		if (mobileDetailDialogEl && !('closedBy' in HTMLDialogElement.prototype)) {
			const el = mobileDetailDialogEl;
			let pressedOutside = false;
			const isOutside = (e: PointerEvent) => {
				const r = el.getBoundingClientRect();
				return (
					e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom
				);
			};
			const onDown = (e: PointerEvent) => {
				pressedOutside = el.open && isOutside(e);
			};
			const onUp = (e: PointerEvent) => {
				if (pressedOutside && el.open && isOutside(e)) {
					el.close();
				}
				pressedOutside = false;
			};
			el.addEventListener('pointerdown', onDown);
			el.addEventListener('pointerup', onUp);
			removeDialogLightDismiss = () => {
				el.removeEventListener('pointerdown', onDown);
				el.removeEventListener('pointerup', onUp);
			};
		}

		return () => {
			stopListening();
			window.removeEventListener('online', onOnline);
			window.removeEventListener('offline', onOffline);
			mql.removeEventListener('change', onMediaChange);
			removeDialogLightDismiss?.();
			if (announceTimer) {
				clearTimeout(announceTimer);
			}
		};
	});

	let accountById = $derived(new Map(accounts.map((a) => [a.id, a])));

	let customGroupColors = $state<Record<string, number>>(
		typeof window !== 'undefined'
			? (() => {
					try {
						const raw = localStorage.getItem('budget-group-colors');
						return raw ? (JSON.parse(raw) as Record<string, number>) : {};
					} catch {
						return {};
					}
				})()
			: {}
	);

	let emptyGroups = $state<string[]>(
		typeof window !== 'undefined'
			? (() => {
					try {
						const raw = localStorage.getItem('budget-empty-groups');
						return raw ? (JSON.parse(raw) as string[]) : [];
					} catch {
						return [];
					}
				})()
			: []
	);

	let groupOrder = $state<string[]>(
		typeof window !== 'undefined'
			? (() => {
					try {
						const raw = localStorage.getItem('budget-group-order');
						return raw ? (JSON.parse(raw) as string[]) : [];
					} catch {
						return [];
					}
				})()
			: []
	);

	function persistGroupColors(next: Record<string, number>) {
		customGroupColors = next;
		if (typeof window !== 'undefined') {
			try {
				localStorage.setItem('budget-group-colors', JSON.stringify(next));
			} catch {
				// ignore storage quota errors
			}
		}
	}

	function persistEmptyGroups(next: string[]) {
		emptyGroups = next;
		if (typeof window !== 'undefined') {
			try {
				localStorage.setItem('budget-empty-groups', JSON.stringify(next));
			} catch {
				// ignore storage quota errors
			}
		}
	}

	function persistGroupOrder(next: string[]) {
		groupOrder = next;
		if (typeof window !== 'undefined') {
			try {
				localStorage.setItem('budget-group-order', JSON.stringify(next));
			} catch {
				// ignore storage quota errors
			}
		}
	}

	function announceReorder(message: string) {
		reorderAnnouncement = '';
		if (announceTimer) {
			clearTimeout(announceTimer);
		}
		announceTimer = setTimeout(() => {
			reorderAnnouncement = message;
			announceTimer = null;
		}, 100);
	}

	function handleChangeGroupColor(groupName: string, colorIndex: number) {
		persistGroupColors({ ...customGroupColors, [groupName]: colorIndex });
	}

	function categoryToUpdatePayload(
		cat: CategoryBudget,
		overrides: Partial<CreateCategoryPayload> = {}
	): CreateCategoryPayload {
		return {
			name: cat.name,
			group_name: cat.groupName,
			monthly_budget: cat.originalPlanned,
			rollover_mode: cat.rolloverMode,
			sweep_target_category_id: cat.sweepTargetCategoryId,
			held_in_account_id: cat.heldInAccountId ?? null,
			target_amount: cat.targetAmount,
			target_date: cat.targetDate,
			target_cadence: cat.targetCadence,
			sort_order: cat.sortOrder,
			...overrides
		};
	}

	async function handleSaveGroup(payload: {
		oldName: string | null;
		name: string;
		colorIndex: number;
	}) {
		const { oldName, name, colorIndex } = payload;

		if (oldName === null) {
			handleChangeGroupColor(name, colorIndex);
			const existsInDb = (budgetData?.categories ?? []).some((c) => c.groupName === name);
			if (!existsInDb && !emptyGroups.includes(name)) {
				persistEmptyGroups([...emptyGroups, name]);
			}
			const baseOrder = groupOrder.length > 0 ? groupOrder : allGroupNames;
			if (!baseOrder.includes(name)) {
				persistGroupOrder([...baseOrder, name]);
			}
			return;
		}

		const nextColors = { ...customGroupColors };
		if (oldName !== name) {
			delete nextColors[oldName];
		}
		nextColors[name] = colorIndex;
		persistGroupColors(nextColors);

		if (oldName !== name) {
			if (emptyGroups.includes(oldName)) {
				persistEmptyGroups(emptyGroups.map((g) => (g === oldName ? name : g)));
			}
			if (groupOrder.includes(oldName)) {
				persistGroupOrder(groupOrder.map((g) => (g === oldName ? name : g)));
			}
			if (oldName in collapsedGroups) {
				const nextCollapsed = { ...collapsedGroups };
				nextCollapsed[name] = nextCollapsed[oldName];
				delete nextCollapsed[oldName];
				collapsedGroups = nextCollapsed;
			}
			if (activeFocusGroup === oldName) {
				activeFocusGroup = name;
			}
			const targets = (budgetData?.categories ?? []).filter((c) => c.groupName === oldName);
			if (targets.length > 0) {
				await Promise.all(
					targets.map((cat) =>
						updateCategory(cat.id, categoryToUpdatePayload(cat, { group_name: name }))
					)
				);
				await loadWorkspace(currentMonth, true);
			}
		}
	}

	async function handleDeleteGroup(groupName: string) {
		const targets = (budgetData?.categories ?? []).filter((c) => c.groupName === groupName);
		if (targets.length > 0) {
			await Promise.all(targets.map((cat) => deleteCategory(cat.id)));
		}
		if (emptyGroups.includes(groupName)) {
			persistEmptyGroups(emptyGroups.filter((g) => g !== groupName));
		}
		if (groupOrder.includes(groupName)) {
			persistGroupOrder(groupOrder.filter((g) => g !== groupName));
		}
		if (groupName in customGroupColors) {
			const nextColors = { ...customGroupColors };
			delete nextColors[groupName];
			persistGroupColors(nextColors);
		}
		if (groupName in collapsedGroups) {
			const nextCollapsed = { ...collapsedGroups };
			delete nextCollapsed[groupName];
			collapsedGroups = nextCollapsed;
		}
		if (activeFocusGroup === groupName) {
			activeFocusGroup = FOCUS_ALL;
		}
		await loadWorkspace(currentMonth, true);
	}

	let groupedCategories = $derived.by(() => {
		const groups = new Map<string, CategoryBudget[]>();
		const dbSorted = [...(budgetData?.categories ?? [])].sort((a, b) => a.sortOrder - b.sortOrder);
		for (const cat of dbSorted) {
			const list = groups.get(cat.groupName) ?? [];
			list.push(cat);
			groups.set(cat.groupName, list);
		}
		for (const list of groups.values()) {
			list.sort((a, b) => a.sortOrder - b.sortOrder);
		}

		const orderedNames = Array.from(groups.keys());
		for (const emptyName of emptyGroups) {
			if (groups.has(emptyName)) continue;
			groups.set(emptyName, []);
			const targetOrderIdx = groupOrder.indexOf(emptyName);
			if (targetOrderIdx === -1) {
				orderedNames.push(emptyName);
				continue;
			}
			const insertAt = orderedNames.findIndex((existing) => {
				const existingIdx = groupOrder.indexOf(existing);
				return existingIdx !== -1 && existingIdx > targetOrderIdx;
			});
			if (insertAt === -1) {
				orderedNames.push(emptyName);
			} else {
				orderedNames.splice(insertAt, 0, emptyName);
			}
		}

		return orderedNames.map((groupName, idx) => ({
			groupName,
			cats: groups.get(groupName) ?? [],
			color: getGroupColor(groupName, customGroupColors[groupName] ?? idx)
		}));
	});

	let allGroupNames = $derived(groupedCategories.map((g) => g.groupName));

	$effect(() => {
		const dbGroups = new Set((budgetData?.categories ?? []).map((c) => c.groupName));
		if (emptyGroups.some((g) => dbGroups.has(g))) {
			persistEmptyGroups(emptyGroups.filter((g) => !dbGroups.has(g)));
		}
	});

	async function persistOrderedCategories(orderedCats: CategoryBudget[]) {
		if (!budgetData) return;
		const prevById = new Map(budgetData.categories.map((c) => [c.id, c]));
		const normalized = orderedCats.map((c, idx) => ({ ...c, sortOrder: idx }));
		const changed = normalized.filter((c) => {
			const prev = prevById.get(c.id);
			return !prev || prev.sortOrder !== c.sortOrder || prev.groupName !== c.groupName;
		});
		if (changed.length === 0) return;

		budgetData = { ...budgetData, categories: normalized };
		try {
			await Promise.all(
				changed.map((cat) => updateCategory(cat.id, categoryToUpdatePayload(cat)))
			);
		} catch (err: any) {
			errorMsg = err?.message || 'Failed to save category order.';
			await loadWorkspace(currentMonth, true);
		}
	}

	async function handleReorderCategory(
		draggedId: string,
		targetId: string,
		position: 'before' | 'after'
	) {
		if (!budgetData || draggedId === targetId) return;
		const dragged = budgetData.categories.find((c) => c.id === draggedId);
		const target = budgetData.categories.find((c) => c.id === targetId);
		if (!dragged || !target) return;

		const updatedDragged: CategoryBudget = { ...dragged, groupName: target.groupName };
		const nextGroups = groupedCategories.map((g) => {
			const withoutDragged = g.cats.filter((c) => c.id !== draggedId);
			if (g.groupName !== target.groupName) {
				return withoutDragged;
			}
			const targetIdx = withoutDragged.findIndex((c) => c.id === targetId);
			if (targetIdx === -1) {
				return [...withoutDragged, updatedDragged];
			}
			const insertAt = position === 'after' ? targetIdx + 1 : targetIdx;
			const next = [...withoutDragged];
			next.splice(insertAt, 0, updatedDragged);
			return next;
		});

		await persistOrderedCategories(nextGroups.flat());
		announceReorder(`Moved ${dragged.name} ${position} ${target.name}`);
	}

	async function handleMoveCategoryToGroup(draggedId: string, targetGroupName: string) {
		if (!budgetData) return;
		const dragged = budgetData.categories.find((c) => c.id === draggedId);
		if (!dragged || dragged.groupName === targetGroupName) return;

		const updatedDragged: CategoryBudget = { ...dragged, groupName: targetGroupName };
		const nextGroups = groupedCategories.map((g) => {
			const withoutDragged = g.cats.filter((c) => c.id !== draggedId);
			if (g.groupName === targetGroupName) {
				return [...withoutDragged, updatedDragged];
			}
			return withoutDragged;
		});

		await persistOrderedCategories(nextGroups.flat());
		announceReorder(`Moved ${dragged.name} to ${targetGroupName}`);
	}

	async function handleMoveCategoryStep(categoryId: string, direction: 'up' | 'down') {
		if (!budgetData) return;
		const group = groupedCategories.find((g) => g.cats.some((c) => c.id === categoryId));
		if (!group) return;
		const idx = group.cats.findIndex((c) => c.id === categoryId);
		const swapIdx = direction === 'up' ? idx - 1 : idx + 1;
		if (swapIdx < 0 || swapIdx >= group.cats.length) return;
		const movedItem = group.cats[idx];

		const nextGroups = groupedCategories.map((g) => {
			if (g.groupName !== group.groupName) return g.cats;
			const next = [...g.cats];
			const [moved] = next.splice(idx, 1);
			next.splice(swapIdx, 0, moved);
			return next;
		});

		await persistOrderedCategories(nextGroups.flat());
		announceReorder(
			`Moved ${movedItem.name} ${direction} to position ${swapIdx + 1} of ${group.cats.length} in ${group.groupName}`
		);
	}

	async function handleReorderGroup(
		draggedGroupName: string,
		targetGroupName: string,
		position: 'before' | 'after'
	) {
		if (!budgetData || draggedGroupName === targetGroupName) return;
		const draggedGroup = groupedCategories.find((g) => g.groupName === draggedGroupName);
		if (!draggedGroup) return;

		const withoutDragged = groupedCategories.filter((g) => g.groupName !== draggedGroupName);
		const targetIdx = withoutDragged.findIndex((g) => g.groupName === targetGroupName);
		if (targetIdx === -1) return;

		const insertAt = position === 'after' ? targetIdx + 1 : targetIdx;
		const nextGroups = [...withoutDragged];
		nextGroups.splice(insertAt, 0, draggedGroup);

		persistGroupOrder(nextGroups.map((g) => g.groupName));
		await persistOrderedCategories(nextGroups.flatMap((g) => g.cats));
		announceReorder(`Moved category ${draggedGroupName} ${position} ${targetGroupName}`);
	}

	async function handleMoveGroupStep(groupName: string, direction: 'up' | 'down') {
		if (!budgetData) return;
		const idx = groupedCategories.findIndex((g) => g.groupName === groupName);
		if (idx === -1) return;
		const swapIdx = direction === 'up' ? idx - 1 : idx + 1;
		if (swapIdx < 0 || swapIdx >= groupedCategories.length) return;

		const nextGroups = [...groupedCategories];
		const [moved] = nextGroups.splice(idx, 1);
		nextGroups.splice(swapIdx, 0, moved);

		persistGroupOrder(nextGroups.map((g) => g.groupName));
		await persistOrderedCategories(nextGroups.flatMap((g) => g.cats));
		announceReorder(
			`Moved category ${groupName} ${direction} to position ${swapIdx + 1} of ${nextGroups.length}`
		);
	}

	function toggleGroupCollapsed(groupName: string) {
		collapsedGroups = { ...collapsedGroups, [groupName]: !collapsedGroups[groupName] };
	}

	let allGroupsCollapsed = $derived(
		groupedCategories.length > 0 && groupedCategories.every((g) => Boolean(collapsedGroups[g.groupName]))
	);

	function toggleAllGroupsCollapsed() {
		const nextVal = !allGroupsCollapsed;
		const next: Record<string, boolean> = {};
		for (const g of groupedCategories) {
			next[g.groupName] = nextVal;
		}
		collapsedGroups = next;
	}

	function toggleOrganizeMode() {
		const next = !organizeMode;
		organizeMode = next;
		if (next) {
			activeFocusGroup = FOCUS_ALL;
		} else {
			collapsedGroups = {};
		}
	}

	let focusGroupNames = $derived(groupedCategories.map((g) => g.groupName));

	let effectiveFocusGroup = $derived(
		activeFocusGroup === FOCUS_ALL ||
			activeFocusGroup === FOCUS_INCOME ||
			focusGroupNames.includes(activeFocusGroup)
			? activeFocusGroup
			: FOCUS_ALL
	);

	let visibleGroupedCategories = $derived.by(() => {
		if (effectiveFocusGroup === FOCUS_ALL) return groupedCategories;
		if (effectiveFocusGroup === FOCUS_INCOME) return [];
		return groupedCategories.filter((g) => g.groupName === effectiveFocusGroup);
	});

	let visibleRecommendations = $derived.by(() => {
		if (isOffline || effectiveFocusGroup === FOCUS_INCOME) return [];
		if (effectiveFocusGroup === FOCUS_ALL) return recommendations;
		const visibleCategoryIds = new Set(
			visibleGroupedCategories.flatMap((g) => g.cats.map((c) => c.id))
		);
		return recommendations.filter((r) => visibleCategoryIds.has(r.categoryId));
	});

	let transactionsByCategory = $derived.by(() => {
		const map = new Map<string, InlineActivityItem[]>();
		const pushItem = (catId: string, item: InlineActivityItem) => {
			const list = map.get(catId) ?? [];
			list.push(item);
			map.set(catId, list);
		};

		for (const tx of transactions) {
			if (tx.isTransfer) continue;
			const acct = accountById.get(tx.accountId);
			if (acct && !acct.isOnBudget) continue;
			const merchant = tx.merchantClean?.trim() || tx.merchantRaw;
			const accountName = tx.accountName || acct?.name || 'Account';

			if (tx.splits && tx.splits.length > 0) {
				for (const split of tx.splits) {
					if (!split.categoryId) continue;
					pushItem(split.categoryId, {
						id: `${tx.id}:${split.id}`,
						transactionId: tx.id,
						date: tx.transactionDate,
						merchant,
						accountName,
						amount: split.amount,
						status: tx.status
					});
				}
			} else if (tx.categoryId) {
				pushItem(tx.categoryId, {
					id: tx.id,
					transactionId: tx.id,
					date: tx.transactionDate,
					merchant,
					accountName,
					amount: tx.amount,
					status: tx.status
				});
			}
		}

		for (const q of queuedTransactions) {
			if (!q.transactionDate.startsWith(currentMonth) || !q.categoryId) continue;
			const acct = accountById.get(q.accountId);
			if (acct && !acct.isOnBudget) continue;
			pushItem(q.categoryId, {
				id: q.localId,
				transactionId: q.localId,
				date: q.transactionDate,
				merchant: q.merchantRaw,
				accountName: acct?.name || 'Offline',
				amount: q.amount,
				status: 'PENDING'
			});
		}

		return map;
	});

	let inflowTransactions = $derived.by(() => {
		const items: InlineActivityItem[] = [];
		for (const tx of transactions) {
			if (tx.isTransfer) continue;
			if (tx.amount <= 0 && !tx.plannedIncomeId) continue;
			const acct = accountById.get(tx.accountId);
			if (acct && !acct.isOnBudget) continue;
			items.push({
				id: tx.id,
				transactionId: tx.id,
				plannedIncomeId: tx.plannedIncomeId ?? null,
				date: tx.transactionDate,
				merchant: tx.merchantClean?.trim() || tx.merchantRaw,
				accountName: tx.accountName || acct?.name || 'Account',
				amount: tx.amount,
				status: tx.status
			});
		}
		for (const q of queuedTransactions) {
			if (!q.transactionDate.startsWith(currentMonth) || q.amount <= 0) continue;
			const acct = accountById.get(q.accountId);
			if (acct && !acct.isOnBudget) continue;
			items.push({
				id: q.localId,
				transactionId: q.localId,
				plannedIncomeId: null,
				date: q.transactionDate,
				merchant: q.merchantRaw,
				accountName: acct?.name || 'Offline',
				amount: q.amount,
				status: 'PENDING'
			});
		}
		return items;
	});

	let selectedTransaction = $derived.by(() => {
		if (!selectedTransactionId) return null;
		const offlineMapped = mapOfflineQueuedToTransactions(queuedTransactions, currentMonth);
		return (
			[...offlineMapped, ...transactions, ...toReviewTransactions].find(
				(tx) => tx.id === selectedTransactionId
			) ?? null
		);
	});

	$effect(() => {
		if (!mobileDetailDialogEl) return;
		if (!isDesktop && selectedTransaction !== null) {
			if (!mobileDetailDialogEl.open) {
				mobileDetailDialogEl.showModal();
			}
		} else if (mobileDetailDialogEl.open) {
			mobileDetailDialogEl.close();
		}
	});

	let overspentCategory = $derived.by(() => {
		if (!coverCategoryId || !budgetData) return null;
		const found = budgetData.categories.find((c) => c.id === coverCategoryId);
		if (!found) return null;
		return {
			id: found.id,
			name: found.name,
			remaining: found.remaining,
			originalPlanned: found.originalPlanned
		};
	});

	let availableDonors = $derived.by(() => {
		if (!budgetData) return [];
		return budgetData.categories
			.filter((c) => c.id !== coverCategoryId && c.remaining > 0)
			.map((c) => ({ id: c.id, name: c.name, available: c.remaining }));
	});

	let receivedInflows = $derived.by(() => {
		let total = 0;
		for (const tx of transactions) {
			if (tx.amount > 0 && !tx.isTransfer) {
				const acct = accountById.get(tx.accountId);
				if (!acct || acct.isOnBudget) {
					total += tx.amount;
				}
			}
		}
		for (const q of queuedTransactions) {
			if (q.transactionDate.startsWith(currentMonth) && q.amount > 0) {
				const acct = accountById.get(q.accountId);
				if (!acct || acct.isOnBudget) {
					total += q.amount;
				}
			}
		}
		return total;
	});

	let allToReviewPool = $derived.by(() => {
		const seen = new Set<string>();
		const out: TransactionItemModel[] = [];
		for (const tx of [...toReviewTransactions, ...transactions]) {
			if (!seen.has(tx.id)) {
				seen.add(tx.id);
				out.push(tx);
			}
		}
		return out;
	});

	let toReviewCount = $derived.by(() => {
		const offlineMapped = mapOfflineQueuedToTransactions(queuedTransactions, currentMonth);
		let count = 0;
		for (const tx of [...offlineMapped, ...allToReviewPool]) {
			if (isToReviewTransaction(tx, accountById)) {
				count++;
			}
		}
		return count;
	});

	let catchUpItems = $derived(
		allToReviewPool.filter((tx) => isCatchUpTransaction(tx, accountById))
	);

	let modalCatchUpItems = $derived.by(() =>
		catchUpItems.length > 0
			? catchUpItems
			: allToReviewPool.filter(
					(tx) =>
						isToReviewTransaction(tx, accountById) &&
						isCategorizableTransaction(tx, accountById)
				)
	);

	function fmtMoney(n: number): string {
		return Math.abs(n).toLocaleString('en-US', {
			minimumFractionDigits: 2,
			maximumFractionDigits: 2
		});
	}

	function handleMonthChange(nextMonth: string) {
		void goto(`/budget?month=${nextMonth}`, {
			replaceState: false,
			keepFocus: true,
			noScroll: true
		});
	}

	function openEditCategory(catId: string) {
		const found = budgetData?.categories.find((c) => c.id === catId) ?? null;
		editingCategory = found;
		modalDefaultGroup = found?.groupName ?? 'Essentials';
		isCategoryModalOpen = true;
	}

	function openNewCategoryInGroup(groupName: string) {
		editingCategory = null;
		modalDefaultGroup = groupName;
		isCategoryModalOpen = true;
	}

	function openNewGroup() {
		editingGroupName = null;
		editingGroupColorIndex = groupedCategories.length % GROUP_COLOR_PALETTE.length;
		isGroupModalOpen = true;
	}

	function openEditGroup(groupName: string) {
		const idx = groupedCategories.findIndex((g) => g.groupName === groupName);
		editingGroupName = groupName;
		editingGroupColorIndex = customGroupColors[groupName] ?? (idx >= 0 ? idx : 0);
		isGroupModalOpen = true;
	}

	function handleSelectTransaction(txId: string, opts?: { openSplit?: boolean }) {
		selectedTransactionId = txId;
		detailOpenSplitMode = Boolean(opts?.openSplit);
		if (!isDesktop) {
			isMobileDrawerOpen = false;
		}
	}

	async function handleUpdatePlanned(catId: string, newPlanned: number) {
		await updateMonthlyPlanned(catId, currentMonth, newPlanned);
		await loadWorkspace(currentMonth, true);
	}

	async function handleCategorizeTransaction(txId: string, categoryId: string | null) {
		const queuedItem = queuedTransactions.find((q) => q.localId === txId);
		if (queuedItem) {
			await localDB.updateQueuedTransaction(txId, {
				categoryId,
				syncStatus: 'QUEUED'
			});
			queuedTransactions = await localDB.getPendingQueue(demo.workspaceId || undefined);
			return;
		}
		await updateTransaction(
			txId,
			categoryId ? { categoryId } : { clearCategory: true }
		);
		await loadWorkspace(currentMonth, true);
	}

	async function handleAssignTransactionToIncome(txId: string, incomeId: string | null) {
		await updateTransaction(
			txId,
			incomeId ? { plannedIncomeId: incomeId, clearCategory: true } : { clearPlannedIncome: true }
		);
		await loadWorkspace(currentMonth, true);
	}

	async function executeDropToTarget(
		tx: TransactionItemModel,
		targetType: 'category' | 'income',
		targetId: string,
		moveToCurrentMonth: boolean
	) {
		const monthPatch = moveToCurrentMonth ? buildMoveToMonthPatch(tx, currentMonth) : {};
		const targetPatch =
			targetType === 'category'
				? { categoryId: targetId }
				: { plannedIncomeId: targetId, clearCategory: true };
		await updateTransaction(tx.id, { ...targetPatch, ...monthPatch });
		await loadWorkspace(currentMonth, true);
	}

	async function handleDropTransactionToCategory(txId: string, categoryId: string) {
		const tx = allToReviewPool.find((t) => t.id === txId) ?? null;
		if (tx && effectiveBudgetMonth(tx) < currentMonth) {
			if (isMoveMonthPromptSkippedToday()) {
				await executeDropToTarget(tx, 'category', categoryId, true);
				return;
			}
			const catName = budgetData?.categories.find((c) => c.id === categoryId)?.name ?? 'this category';
			pendingMoveDrop = {
				tx,
				fromMonth: effectiveBudgetMonth(tx),
				targetType: 'category',
				targetId: categoryId,
				targetLabel: catName
			};
			return;
		}
		await handleCategorizeTransaction(txId, categoryId);
	}

	async function handleDropTransactionToIncome(txId: string, incomeId: string) {
		const tx = allToReviewPool.find((t) => t.id === txId) ?? null;
		if (tx && effectiveBudgetMonth(tx) < currentMonth) {
			if (isMoveMonthPromptSkippedToday()) {
				await executeDropToTarget(tx, 'income', incomeId, true);
				return;
			}
			const incName = budgetData?.incomes.find((i) => i.id === incomeId)?.name ?? 'Income';
			pendingMoveDrop = {
				tx,
				fromMonth: effectiveBudgetMonth(tx),
				targetType: 'income',
				targetId: incomeId,
				targetLabel: incName
			};
			return;
		}
		await handleAssignTransactionToIncome(txId, incomeId);
	}

	async function handleConfirmMoveDrop(dontRemindToday: boolean) {
		if (!pendingMoveDrop || isSubmittingMoveDrop) return;
		isSubmittingMoveDrop = true;
		try {
			if (dontRemindToday) {
				skipMoveMonthPromptToday();
			}
			const drop = pendingMoveDrop;
			pendingMoveDrop = null;
			await executeDropToTarget(drop.tx, drop.targetType, drop.targetId, true);
		} finally {
			isSubmittingMoveDrop = false;
		}
	}

	async function handleUpdateTransaction(txId: string, payload: UpdateTransactionPayload) {
		const queuedItem = queuedTransactions.find((q) => q.localId === txId);
		if (queuedItem) {
			const patch: Parameters<BudgetLocalDB['updateQueuedTransaction']>[1] = {
				syncStatus: 'QUEUED'
			};
			if (payload.merchantClean !== undefined) patch.merchantRaw = payload.merchantClean;
			if (payload.userNotes !== undefined) patch.userNotes = payload.userNotes;
			if (payload.amount !== undefined) patch.amount = payload.amount;
			if (payload.clearCategory) {
				patch.categoryId = null;
			} else if (payload.categoryId !== undefined) {
				patch.categoryId = payload.categoryId;
			}
			await localDB.updateQueuedTransaction(txId, patch);
			queuedTransactions = await localDB.getPendingQueue(demo.workspaceId || undefined);
			return;
		}
		await updateTransaction(txId, payload);
		await loadWorkspace(currentMonth, true);
	}

	async function handleReplaceSplits(txId: string, payload: ReplaceSplitsPayload) {
		await replaceTransactionSplits(txId, payload);
		detailOpenSplitMode = false;
		await loadWorkspace(currentMonth, true);
	}

	async function handleApproveReview(txId: string) {
		await updateTransaction(txId, { approveReview: true });
		await loadWorkspace(currentMonth, true);
	}

	async function handleIgnoreReviewBefore(beforeMonth: string): Promise<number> {
		const res = await ignoreReviewBefore(beforeMonth);
		await loadWorkspace(currentMonth, true);
		return res.updated;
	}

	async function handleApplySimilar(payload: ApplySimilarPayload): Promise<number> {
		const count = await applySimilarCategorization(payload);
		await loadWorkspace(currentMonth, true);
		return count;
	}

	function handleCreateRuleFromTx(
		tx: TransactionItemModel,
		opts?: { categoryId?: string | null; plannedIncomeName?: string | null }
	) {
		const incomeName =
			opts?.plannedIncomeName ??
			(tx.plannedIncomeId
				? budgetData?.incomes.find((i) => i.id === tx.plannedIncomeId)?.name ?? null
				: null);
		ruleModalPrefill = {
			matchType: 'CONTAINS',
			merchantPattern: tx.merchantClean?.trim() || tx.merchantRaw,
			accountId: null,
			direction: 'ANY',
			categoryId: incomeName ? null : opts?.categoryId ?? tx.categoryId ?? null,
			plannedIncomeName: incomeName,
			applyNow: true
		};
		isRuleModalOpen = true;
	}

	async function queueTransactionLocally(payload: {
		accountId: string;
		amount: number;
		merchantRaw: string;
		categoryId: string | null;
		transactionDate: string;
	}) {
		const offlineTx: OfflineTransaction = {
			localId: `offline-${crypto.randomUUID()}`,
			workspaceId: demo.workspaceId || undefined,
			accountId: payload.accountId,
			amount: payload.amount,
			merchantRaw: payload.merchantRaw,
			categoryId: payload.categoryId,
			transactionDate: payload.transactionDate,
			syncStatus: 'QUEUED',
			createdAt: Date.now()
		};
		await localDB.queueTransaction(offlineTx);
		if (payload.categoryId && budgetData) {
			budgetData = {
				...budgetData,
				categories: budgetData.categories.map((c) =>
					c.id === payload.categoryId
						? { ...c, remaining: c.remaining + payload.amount }
						: c
				)
			};
			await localDB.cacheCategories(
				budgetData.categories.map((c) => ({
					id: c.id,
					name: c.name,
					groupName: c.groupName,
					remaining: c.remaining,
					originalPlanned: c.originalPlanned,
					effectiveTarget: c.effectiveTarget,
					spentCleared: c.spentCleared,
					spentPending: c.spentPending,
					sortOrder: c.sortOrder
				}))
			);
		}
		await requestBackgroundSync();
		queuedTransactions = await localDB.getPendingQueue(demo.workspaceId || undefined);
	}

	async function handleCreateTransaction(payload: {
		accountId: string;
		amount: number;
		merchantRaw: string;
		categoryId: string | null;
		transactionDate: string;
	}) {
		if ((typeof navigator !== 'undefined' && !navigator.onLine) || !auth.isAuthenticated) {
			await queueTransactionLocally(payload);
			return;
		}

		try {
			await createTransaction({
				accountId: payload.accountId,
				totalAmount: payload.amount,
				merchantRaw: payload.merchantRaw,
				categoryId: payload.categoryId,
				transactionDate: payload.transactionDate
			});
		} catch (err) {
			if (isNetworkError(err)) {
				isOffline = true;
				await queueTransactionLocally(payload);
				return;
			}
			throw err;
		}
		await loadWorkspace(currentMonth, true);
	}

	async function handleDeleteTransaction(txId: string) {
		const queuedItem = queuedTransactions.find((q) => q.localId === txId);
		if (queuedItem) {
			await localDB.deleteQueuedTransaction(txId);
			queuedTransactions = await localDB.getPendingQueue(demo.workspaceId || undefined);
			if (selectedTransactionId === txId) {
				selectedTransactionId = null;
			}
			return;
		}
		await deleteTransaction(txId);
		await loadWorkspace(currentMonth, true);
	}

	async function handleSyncNow() {
		syncing = true;
		try {
			const flushed = await flushOfflineQueue(localDB, undefined, undefined, {
				workspaceId: demo.workspaceId || undefined
			});
			if (typeof navigator === 'undefined' || navigator.onLine) {
				await demo.triggerSyncAndPoll();
			}
			await loadWorkspace(currentMonth, true);
			// The replay is durable now, so an item can be refused for a reason only the
			// user can fix — a deleted account, a date that never parsed. Say which, rather
			// than leaving it sitting in the queue looking like it is still on its way.
			if (flushed.failedCount > 0) {
				const first = flushed.rejections.find((r) => !r.retryable);
				errorMsg = `${flushed.failedCount} queued transaction${flushed.failedCount === 1 ? '' : 's'} could not be saved${
					first ? ` (${first.reason})` : ''
				} and stayed in the queue for editing.`;
			}
		} catch (err: any) {
			errorMsg = err?.message || 'Sync failed.';
		} finally {
			syncing = false;
		}
	}

	function openCatchUpStack() {
		if (modalCatchUpItems.length > 0) {
			catchUpModalMode = 'stack';
			isCatchUpModalOpen = true;
		} else {
			isMobileDrawerOpen = true;
		}
	}
</script>

<BudgetHeader
	{currentMonth}
	summary={budgetData?.summary ?? null}
	{solvency}
	accountCount={accounts.length}
	{toReviewCount}
	{isOffline}
	queuedCount={queuedTransactions.length}
	syncing={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
	isDrawerOpen={isMobileDrawerOpen}
	onSyncNow={handleSyncNow}
	onMonthChange={handleMonthChange}
	onOpenCatchUp={openCatchUpStack}
	onToggleTransactionsDrawer={() => (isMobileDrawerOpen = !isMobileDrawerOpen)}
	onSignOut={() => {
		demo.resetOnLogout();
		void auth.logout();
	}}
/>

<div class="max-w-[1440px] mx-auto px-3 sm:px-6 py-4 sm:py-5">
	<!-- Hybrid 12-Column Split Grid: 8 cols Budget | 4 cols Sticky Transactions (lg+) -->
	<div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
		<!-- Left/Main Column: Budget Canvas -->
		<div class="lg:col-span-8 space-y-5 min-w-0">
			<div class="flex flex-wrap items-center justify-between gap-3">
				<div class="flex flex-wrap items-center gap-3 text-xs text-ink-secondary">
					{#if budgetData?.summary}
						<span>
							Planned:
							<strong class="font-mono text-ink-primary">
								${fmtMoney(budgetData.summary.totalAllocated)}
							</strong>
						</span>
						<span>
							Spent:
							<strong class="font-mono text-ink-primary">
								${fmtMoney(budgetData.summary.totalSpentCleared + budgetData.summary.totalSpentPending)}
							</strong>
						</span>
						{#if budgetData.summary.isUnbalanced}
							<span class="rounded-full bg-status-danger-bg text-status-danger px-2 py-0.5 text-[11px] font-semibold">
								Overspent Categories
							</span>
						{/if}
					{/if}
				</div>

				<div class="flex items-center gap-2">
					{#if groupedCategories.length > 0}
						<button
							type="button"
							data-testid="toggle-organize-btn"
							aria-pressed={organizeMode}
							onclick={toggleOrganizeMode}
							class="rounded-lg border px-3 py-2 text-xs font-semibold transition-colors {organizeMode
								? 'border-accent-pop bg-accent-tint text-accent-pop'
								: 'border-border bg-surface text-ink-secondary hover:bg-surface-hover hover:text-ink-primary'}"
						>
							{organizeMode ? 'Done' : 'Organize'}
						</button>
					{/if}
					<button
						type="button"
						data-testid="add-category-btn"
						onclick={openNewGroup}
						class="rounded-lg bg-accent-primary text-accent-primary-text px-3.5 py-2 text-xs font-semibold hover:opacity-90 transition-opacity"
					>
						+ Add Category
					</button>
				</div>
			</div>

			<div class="sr-only" aria-live="polite" role="status">{reorderAnnouncement}</div>

			{#if organizeMode && groupedCategories.length > 0}
				<div
					data-testid="organize-mode-banner"
					class="rounded-xl border border-accent-pop/30 bg-accent-tint px-3.5 py-2.5 flex flex-wrap items-center justify-between gap-2 text-xs"
				>
					<span class="text-ink-primary font-medium">
						Use the arrows to reorder categories and items, or pick a new category for an item.
					</span>
					<div class="flex items-center gap-2 shrink-0">
						<button
							type="button"
							data-testid="organize-collapse-all-btn"
							onclick={toggleAllGroupsCollapsed}
							class="rounded-lg border border-border bg-surface px-2.5 py-1 text-xs font-medium text-ink-primary hover:bg-surface-hover transition-colors"
						>
							{allGroupsCollapsed ? 'Expand All' : 'Collapse All'}
						</button>
						<button
							type="button"
							onclick={toggleOrganizeMode}
							class="rounded-lg bg-accent-primary text-accent-primary-text px-3 py-1 text-xs font-semibold hover:opacity-90 transition-opacity"
						>
							Done
						</button>
					</div>
				</div>
			{/if}

			{#if errorMsg}
				<div class="rounded-xl border border-status-danger bg-status-danger-bg p-4 text-xs text-status-danger">
					{errorMsg}
				</div>
			{/if}

			{#if isLoading && !budgetData}
				<div class="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-ink-secondary">
					Loading {currentMonth} budget...
				</div>
			{:else}
				<CategoryFocusPills
					groups={focusGroupNames}
					selected={effectiveFocusGroup}
					onSelect={(value) => (activeFocusGroup = value)}
				/>

				<RecommendationBanner
					recommendations={visibleRecommendations}
					yearMonth={currentMonth}
					onApplied={() => void loadWorkspace(currentMonth, true)}
				/>

				{#if effectiveFocusGroup === FOCUS_ALL || effectiveFocusGroup === FOCUS_INCOME}
					<IncomeCard
						yearMonth={`${currentMonth}-01`}
						incomes={budgetData?.incomes ?? []}
						receivedAmount={receivedInflows}
						{inflowTransactions}
						{selectedTransactionId}
						{draggingGroupName}
						{draggingTxId}
						onSelectTransaction={handleSelectTransaction}
						onDropTransaction={handleDropTransactionToIncome}
						onChanged={() => void loadWorkspace(currentMonth, true)}
					/>
				{/if}

				{#if effectiveFocusGroup !== FOCUS_INCOME}
					{#if groupedCategories.length === 0}
						<div class="rounded-2xl border border-border bg-surface p-8 text-center space-y-3">
							<p class="text-sm font-medium text-ink-primary">No categories yet</p>
							<p class="text-xs text-ink-secondary">
								Click "+ Add Category" above to create a category, then add budget items inside it.
							</p>
						</div>
					{:else}
						<div class="space-y-5">
							{#each visibleGroupedCategories as group, groupIdx (group.groupName)}
								<CategoryGroupCard
									groupName={group.groupName}
									color={group.color}
									categories={group.cats}
									{accountById}
									{displayMode}
									{transactionsByCategory}
									{draggingCategoryId}
									{draggingGroupName}
									{draggingTxId}
									{organizeMode}
									isCollapsed={Boolean(collapsedGroups[group.groupName])}
									isFirstGroup={groupIdx === 0}
									isLastGroup={groupIdx === visibleGroupedCategories.length - 1}
									availableGroups={allGroupNames}
									{selectedTransactionId}
									onToggleDisplayMode={() =>
										(displayMode = displayMode === 'remaining' ? 'spent' : 'remaining')}
									onCoverClick={(id) => (coverCategoryId = id)}
									onEditClick={openEditCategory}
									onEditGroup={openEditGroup}
									onUpdatePlanned={handleUpdatePlanned}
									onAddCategoryInGroup={openNewCategoryInGroup}
									onDropTransaction={handleDropTransactionToCategory}
									onChangeGroupColor={handleChangeGroupColor}
									onCategoryDragStart={(id) => (draggingCategoryId = id)}
									onCategoryDragEnd={() => (draggingCategoryId = null)}
									onReorderCategory={handleReorderCategory}
									onMoveCategoryToGroup={handleMoveCategoryToGroup}
									onMoveCategoryStep={handleMoveCategoryStep}
									onGroupDragStart={(name) => (draggingGroupName = name)}
									onGroupDragEnd={() => (draggingGroupName = null)}
									onReorderGroup={handleReorderGroup}
									onMoveGroupStep={handleMoveGroupStep}
									onToggleCollapse={toggleGroupCollapsed}
									onSelectTransaction={handleSelectTransaction}
								/>
							{/each}
						</div>
					{/if}
				{/if}
			{/if}
		</div>

		<!-- Right Column (Desktop lg+): Dedicated Sticky Transactions / Detail Panel -->
		<aside
			aria-label="Transactions"
			data-testid="desktop-transactions-panel"
			class="hidden lg:flex lg:col-span-4 lg:sticky lg:top-[4.75rem] flex-col rounded-2xl border border-border bg-surface shadow-sm overflow-hidden lg:h-[calc(100dvh-6rem)]"
		>
			{#if selectedTransaction}
				<TransactionDetailCard
					variant="panel"
					transaction={selectedTransaction}
					account={accountById.get(selectedTransaction.accountId) ?? null}
					categories={budgetData?.categories ?? []}
					incomes={budgetData?.incomes ?? []}
					suggestions={suggestionsByTxId[selectedTransaction.id] ?? []}
					initialSplitMode={detailOpenSplitMode}
					onClose={() => {
						selectedTransactionId = null;
						detailOpenSplitMode = false;
					}}
					onUpdateTransaction={handleUpdateTransaction}
					onReplaceSplits={handleReplaceSplits}
					onApproveReview={handleApproveReview}
					onCreateRule={handleCreateRuleFromTx}
					onDeleteTransaction={handleDeleteTransaction}
				/>
			{/if}

			<div class="flex-1 min-h-0 flex flex-col {selectedTransaction ? 'hidden' : ''}">
				<div class="px-4 py-3 border-b border-border bg-subtle flex items-center justify-between shrink-0">
					<h2 class="text-xs font-bold uppercase tracking-wider text-ink-primary">Transactions</h2>
					{#if toReviewCount > 0}
						<span class="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full text-[11px] font-bold bg-accent-pop text-accent-primary-text">
							{toReviewCount}
						</span>
					{/if}
				</div>

				<div class="flex-1 min-h-0 flex flex-col">
					<TransactionsPanel
						{currentMonth}
						{transactions}
						{toReviewTransactions}
						{queuedTransactions}
						{accounts}
						categories={budgetData?.categories ?? []}
						incomes={budgetData?.incomes ?? []}
						{suggestionsByTxId}
						{isOffline}
						syncing={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
						{activeCategoryFilterId}
						onClearCategoryFilter={() => (activeCategoryFilterId = null)}
						onSelectTransaction={handleSelectTransaction}
						onCreateTransaction={handleCreateTransaction}
						onCategorizeTransaction={handleCategorizeTransaction}
						onAssignTransactionToIncome={handleAssignTransactionToIncome}
						onApproveReview={handleApproveReview}
						onCreateRule={handleCreateRuleFromTx}
						onApplySimilar={handleApplySimilar}
						onUpdateTransaction={handleUpdateTransaction}
						onIgnoreReviewBefore={handleIgnoreReviewBefore}
						onDeleteTransaction={handleDeleteTransaction}
						onSyncNow={handleSyncNow}
						onTransactionDragStart={(id) => (draggingTxId = id)}
						onTransactionDragEnd={() => (draggingTxId = null)}
					/>
				</div>
			</div>
		</aside>
	</div>
</div>

<TransactionsDrawer
	isOpen={isMobileDrawerOpen}
	title="Transactions"
	badgeCount={toReviewCount}
	onClose={() => (isMobileDrawerOpen = false)}
>
	<TransactionsPanel
		{currentMonth}
		{transactions}
		{toReviewTransactions}
		{queuedTransactions}
		{accounts}
		categories={budgetData?.categories ?? []}
		incomes={budgetData?.incomes ?? []}
		{suggestionsByTxId}
		{isOffline}
		syncing={syncing || demo.syncStatus === 'running' || demo.syncStatus === 'retrying'}
		{activeCategoryFilterId}
		onClearCategoryFilter={() => (activeCategoryFilterId = null)}
		onSelectTransaction={handleSelectTransaction}
		onCreateTransaction={handleCreateTransaction}
		onCategorizeTransaction={handleCategorizeTransaction}
		onAssignTransactionToIncome={handleAssignTransactionToIncome}
		onApproveReview={handleApproveReview}
		onCreateRule={handleCreateRuleFromTx}
		onApplySimilar={handleApplySimilar}
		onUpdateTransaction={handleUpdateTransaction}
		onIgnoreReviewBefore={handleIgnoreReviewBefore}
		onDeleteTransaction={handleDeleteTransaction}
		onSyncNow={handleSyncNow}
		onTransactionDragStart={(id) => (draggingTxId = id)}
		onTransactionDragEnd={() => (draggingTxId = null)}
	/>
</TransactionsDrawer>

<CoverDrawer
	isOpen={coverCategoryId !== null}
	yearMonth={`${currentMonth}-01`}
	{overspentCategory}
	{availableDonors}
	readyToAssign={budgetData?.summary.readyToAssign ?? 0}
	onClose={() => (coverCategoryId = null)}
	onSuccess={() => {
		coverCategoryId = null;
		void loadWorkspace(currentMonth, true);
	}}
/>

<CategoryModal
	isOpen={isCategoryModalOpen}
	{editingCategory}
	allCategories={budgetData?.categories ?? []}
	{accounts}
	availableGroups={allGroupNames}
	defaultGroupName={modalDefaultGroup}
	yearMonth={currentMonth}
	onClose={() => (isCategoryModalOpen = false)}
	onSuccess={async () => {
		isCategoryModalOpen = false;
		await loadWorkspace(currentMonth, true);
		if (groupOrder.length > 0 && budgetData) {
			const byGroup = new Map<string, CategoryBudget[]>();
			for (const cat of [...budgetData.categories].sort((a, b) => a.sortOrder - b.sortOrder)) {
				const list = byGroup.get(cat.groupName) ?? [];
				list.push(cat);
				byGroup.set(cat.groupName, list);
			}
			const ordered: CategoryBudget[] = [];
			for (const grp of groupOrder) {
				const items = byGroup.get(grp);
				if (items) {
					ordered.push(...items);
					byGroup.delete(grp);
				}
			}
			for (const items of byGroup.values()) {
				ordered.push(...items);
			}
			await persistOrderedCategories(ordered);
		}
	}}
/>

<CategoryGroupModal
	isOpen={isGroupModalOpen}
	editingGroupName={editingGroupName}
	initialColorIndex={editingGroupColorIndex}
	existingGroupNames={allGroupNames}
	itemCount={editingGroupName
		? (groupedCategories.find((g) => g.groupName === editingGroupName)?.cats.length ?? 0)
		: 0}
	onClose={() => (isGroupModalOpen = false)}
	onSave={handleSaveGroup}
	onDelete={handleDeleteGroup}
/>

<!-- Mobile Transaction Detail Pop-Up -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={mobileDetailDialogEl}
	aria-label="Transaction details"
	closedby="any"
	onclose={() => {
		if (!isDesktop) {
			selectedTransactionId = null;
		}
	}}
	class="lg:hidden m-auto w-[calc(100%-1.5rem)] max-w-md rounded-2xl border border-border bg-surface p-0 text-ink-primary shadow-2xl max-h-[85dvh] overflow-hidden"
>
	{#if selectedTransaction}
		<div class="flex flex-col max-h-[85dvh]">
			<TransactionDetailCard
				variant="modal"
				transaction={selectedTransaction}
				account={accountById.get(selectedTransaction.accountId) ?? null}
				categories={budgetData?.categories ?? []}
				incomes={budgetData?.incomes ?? []}
				suggestions={suggestionsByTxId[selectedTransaction.id] ?? []}
				initialSplitMode={detailOpenSplitMode}
				onClose={() => {
					selectedTransactionId = null;
					detailOpenSplitMode = false;
				}}
				onUpdateTransaction={handleUpdateTransaction}
				onReplaceSplits={handleReplaceSplits}
				onApproveReview={handleApproveReview}
				onCreateRule={handleCreateRuleFromTx}
				onDeleteTransaction={handleDeleteTransaction}
			/>
		</div>
	{/if}
</dialog>

<CatchUpModal
	isOpen={isCatchUpModalOpen}
	initialMode={catchUpModalMode}
	items={modalCatchUpItems}
	allTransactions={transactions}
	{accounts}
	categories={budgetData?.categories ?? []}
	{suggestionsByTxId}
	onCategorizeTransaction={handleCategorizeTransaction}
	onAssignTransactionToIncome={(txId, incId) => handleAssignTransactionToIncome(txId, incId)}
	onSplitTransaction={(txId) => handleSelectTransaction(txId, { openSplit: true })}
	onClose={() => (isCatchUpModalOpen = false)}
/>

<RuleModal
	isOpen={isRuleModalOpen}
	prefill={ruleModalPrefill}
	categories={budgetData?.categories ?? []}
	incomes={budgetData?.incomes ?? []}
	{accounts}
	onClose={() => (isRuleModalOpen = false)}
	onSaved={() => void loadWorkspace(currentMonth, true)}
/>

<MoveMonthConfirmModal
	isOpen={pendingMoveDrop !== null}
	merchantName={pendingMoveDrop ? (pendingMoveDrop.tx.merchantClean?.trim() || pendingMoveDrop.tx.merchantRaw) : ''}
	fromMonth={pendingMoveDrop?.fromMonth ?? ''}
	toMonth={currentMonth}
	targetLabel={pendingMoveDrop?.targetLabel ?? ''}
	isSubmitting={isSubmittingMoveDrop}
	onCancel={() => (pendingMoveDrop = null)}
	onConfirm={handleConfirmMoveDrop}
/>

<style>
	dialog:modal {
		overscroll-behavior: contain;
	}
	dialog::backdrop {
		overflow: hidden;
		overscroll-behavior: contain;
		background: rgb(0 0 0 / 0.65);
		backdrop-filter: blur(4px);
	}
</style>
