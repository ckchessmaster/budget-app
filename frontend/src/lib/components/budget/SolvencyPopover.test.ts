import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import SolvencyPopover from './SolvencyPopover.svelte';
import type { SolvencyReport } from '$lib/types/transaction';

const healthySolvency: SolvencyReport = {
	totalLiquidCash: 15000.0,
	totalCreditCardDebt: 2695.2,
	solvencyBuffer: 12304.8,
	isFloatingDebt: false,
	floatingDebtAmount: 0
};

const floatingDebtSolvency: SolvencyReport = {
	totalLiquidCash: 1200.0,
	totalCreditCardDebt: 1850.5,
	solvencyBuffer: -650.5,
	isFloatingDebt: true,
	floatingDebtAmount: 650.5
};

describe('SolvencyPopover', () => {
	it('renders compact Net Cash pill and toggles breakdown popover on click', async () => {
		const { getByTestId, queryByTestId, getByText, getByRole } = render(SolvencyPopover, {
			props: {
				solvency: healthySolvency,
				accountCount: 4
			}
		});

		const trigger = getByTestId('solvency-popover-trigger');
		expect(trigger.getAttribute('aria-expanded')).toBe('false');
		expect(trigger.textContent).toContain('Net Cash');
		expect(trigger.textContent).toContain('$12,304.80');
		expect(queryByTestId('solvency-popover-panel')).toBeNull();

		await fireEvent.click(trigger);

		expect(trigger.getAttribute('aria-expanded')).toBe('true');
		const panel = getByTestId('solvency-popover-panel');
		expect(panel).toBeTruthy();
		expect(getByText('Liquid Cash')).toBeTruthy();
		expect(getByText('$15,000.00')).toBeTruthy();
		expect(getByText('Credit Card Debt')).toBeTruthy();
		expect(getByText('$2,695.20')).toBeTruthy();
		expect(getByText('Solvency Buffer')).toBeTruthy();

		const accountsLink = getByRole('link', { name: /Manage Accounts \(4\)/ });
		expect(accountsLink.getAttribute('href')).toBe('/accounts');
	});

	it('closes popover on Escape key and outside pointerdown', async () => {
		const { getByTestId, queryByTestId } = render(SolvencyPopover, {
			props: {
				solvency: healthySolvency
			}
		});

		const trigger = getByTestId('solvency-popover-trigger');
		await fireEvent.click(trigger);
		expect(getByTestId('solvency-popover-panel')).toBeTruthy();

		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(queryByTestId('solvency-popover-panel')).toBeNull();
		expect(trigger.getAttribute('aria-expanded')).toBe('false');

		// Re-open and click outside
		await fireEvent.click(trigger);
		expect(getByTestId('solvency-popover-panel')).toBeTruthy();

		await fireEvent.pointerDown(document.body);
		expect(queryByTestId('solvency-popover-panel')).toBeNull();
	});

	it('displays Floating Debt warning badge and negative net cash when floating debt exists', async () => {
		const { getByTestId, getByText } = render(SolvencyPopover, {
			props: {
				solvency: floatingDebtSolvency
			}
		});

		const trigger = getByTestId('solvency-popover-trigger');
		expect(trigger.textContent).toContain('-$650.50');

		await fireEvent.click(trigger);
		expect(getByText('Floating Debt')).toBeTruthy();
	});

	it('renders nothing when solvency is null', () => {
		const { queryByTestId } = render(SolvencyPopover, {
			props: {
				solvency: null
			}
		});

		expect(queryByTestId('solvency-popover-trigger')).toBeNull();
	});

	it('displays Reserved in Funds and Unreserved Cash when sinking funds hold reserves', async () => {
		const { getByTestId, getByText } = render(SolvencyPopover, {
			props: {
				solvency: {
					...healthySolvency,
					totalReservedInFunds: 3000,
					unreservedCash: 9304.8
				}
			}
		});

		await fireEvent.click(getByTestId('solvency-popover-trigger'));
		expect(getByText('Reserved in Funds')).toBeTruthy();
		expect(getByText('$3,000.00')).toBeTruthy();
		expect(getByText('Unreserved Cash')).toBeTruthy();
		expect(getByText('$9,304.80')).toBeTruthy();
	});
});
