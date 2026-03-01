import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'

export default defineConfig(({ mode }) => {
  const isLib = mode === 'lib'

  return {
    plugins: [react(), tailwindcss()],
    base: process.env.VITE_BASE ?? '/',
    ...(isLib && {
      build: {
        lib: {
          entry: resolve(__dirname, 'src/index.js'),
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
