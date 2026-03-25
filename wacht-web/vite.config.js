import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'

const rootDir = dirname(fileURLToPath(import.meta.url))

export default defineConfig(({ mode }) => {
  const isLib = mode === 'lib'
  const env = loadEnv(mode, rootDir, '')

  return {
    plugins: [react(), tailwindcss()],
    base: env.VITE_BASE ?? '/',
    server: {
      proxy: {
        '/api': 'http://localhost:8080',
        '/status': 'http://localhost:8080',
        '/healthz': 'http://localhost:8080',
      },
    },
    ...(isLib && {
      build: {
        lib: {
          entry: resolve(rootDir, 'src/index.js'),
          name: 'WachtUI',
          fileName: 'wacht-ui',
        },
        rollupOptions: {
          external: ['react', 'react-dom'],
          output: {
            globals: { react: 'React', 'react-dom': 'ReactDOM' },
          },
        },
      },
    }),
  }
})
