import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'
export default defineConfig({ plugins: [vue()], build: { rollupOptions: { input: { admin: resolve(import.meta.dirname, 'index.html'), family: resolve(import.meta.dirname, 'family.html') } } } })
