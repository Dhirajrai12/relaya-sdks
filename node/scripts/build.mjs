// Builds ESM (dist/esm) and CommonJS (dist/cjs) from the same sources.
import { execFileSync } from 'node:child_process'
import { mkdirSync, rmSync, writeFileSync } from 'node:fs'

const tsc = (...args) => execFileSync(process.execPath, ['node_modules/typescript/bin/tsc', ...args], { stdio: 'inherit' })
rmSync('dist', { recursive: true, force: true })
tsc('-p', 'tsconfig.json')
tsc('-p', 'tsconfig.json', '--module', 'commonjs', '--moduleResolution', 'bundler', '--verbatimModuleSyntax', 'false', '--outDir', 'dist/cjs')
mkdirSync('dist/cjs', { recursive: true })
writeFileSync('dist/cjs/package.json', '{ "type": "commonjs" }\n')
writeFileSync('dist/esm/package.json', '{ "type": "module" }\n')
