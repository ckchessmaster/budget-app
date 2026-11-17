import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		host: true,
		port: 5173
	},
	resolve: {
		conditions: ['browser']
	},
	test: {
		environment: 'jsdom',
		globals: true,
		include: ['src/**/*.{test,spec}.{js,ts}']
	}
});
