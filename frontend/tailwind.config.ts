import type { Config } from 'tailwindcss';

export default {
	content: ['./src/**/*.{html,js,svelte,ts}'],
	darkMode: [
		'variant',
		[
			'@media (prefers-color-scheme: dark) { :root:not(:has(> head > meta[name="color-scheme"][content="light"])) & }',
			':root:has(> head > meta[name="color-scheme"][content="dark"]) &'
		]
	],
	theme: {
		extend: {
			height: {
				dvh: '100dvh'
			},
			colors: {
				canvas: 'var(--bg-canvas)',
				surface: 'var(--bg-surface)',
				subtle: 'var(--bg-subtle)',
				'surface-hover': 'var(--bg-hover)',
				border: 'var(--border-default)',
				ink: {
					primary: 'var(--text-primary)',
					secondary: 'var(--text-secondary)',
					muted: 'var(--text-muted)'
				},
				accent: {
					primary: 'var(--accent-primary)',
					'primary-text': 'var(--accent-primary-text)',
					pop: 'var(--accent-pop)',
					tint: 'var(--accent-tint)'
				},
				status: {
					good: 'var(--status-good)',
					'good-bg': 'var(--status-good-bg)',
					warn: 'var(--status-warn)',
					'warn-bg': 'var(--status-warn-bg)',
					danger: 'var(--status-danger)',
					'danger-bg': 'var(--status-danger-bg)'
				}
			},
			borderColor: {
				DEFAULT: 'var(--border-default)'
			}
		}
	},
	plugins: []
} satisfies Config;
