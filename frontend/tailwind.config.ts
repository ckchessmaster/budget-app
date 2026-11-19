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
			}
		}
	},
	plugins: []
} satisfies Config;
