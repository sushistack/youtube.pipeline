import { mkdir, rm } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn } from 'node:child_process'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const repoRoot = path.resolve(__dirname, '../..')
const webRoot = path.resolve(repoRoot, 'web')
const tempRoot = path.resolve(repoRoot, '.tmp/playwright')
const port = process.env.PLAYWRIGHT_PORT ?? '4173'
const binaryPath = path.join(
  tempRoot,
  process.platform === 'win32' ? 'pipeline.exe' : 'pipeline',
)
const dbPath = path.join(tempRoot, 'pipeline.db')
const outputDir = path.join(tempRoot, 'output')

// Wipe per-run state on every server boot so spec runs do not see leftover
// runs from prior invocations. Without this, the run inventory carries the
// last session's POSTed runs into the next /api/runs list, which races with
// new-run-creation.spec.ts and any other spec that asserts on a clean
// inventory state. The Go binary recreates pipeline.db + applies migrations
// at startup, so the wipe is safe.
await rm(dbPath, { force: true })
await rm(`${dbPath}-wal`, { force: true })
await rm(`${dbPath}-shm`, { force: true })
await rm(outputDir, { recursive: true, force: true })
await mkdir(outputDir, { recursive: true })

async function runToCompletion(command, args, options = {}) {
  const child = spawn(command, args, { stdio: 'inherit', ...options })
  const forward = (signal) => {
    if (!child.killed) child.kill(signal)
  }
  process.on('SIGINT', forward)
  process.on('SIGTERM', forward)
  try {
    await new Promise((resolve, reject) => {
      child.on('error', reject)
      child.on('exit', (code, signal) => {
        if (signal) reject(new Error(`${command} killed by ${signal}`))
        else if (code !== 0) reject(new Error(`${command} exited with code ${code}`))
        else resolve()
      })
    })
  } finally {
    process.off('SIGINT', forward)
    process.off('SIGTERM', forward)
  }
}

try {
  await runToCompletion('npm', ['run', 'build'], { cwd: webRoot })
  await runToCompletion('go', ['build', '-o', binaryPath, './cmd/pipeline'], {
    cwd: repoRoot,
  })
} catch (err) {
  console.error(err.message)
  process.exit(1)
}

const serve = spawn(binaryPath, ['serve', '--port', port], {
  cwd: repoRoot,
  stdio: 'inherit',
  env: {
    ...process.env,
    DATA_DIR: path.resolve(repoRoot, 'testdata'),
    DB_PATH: path.join(tempRoot, 'pipeline.db'),
    OUTPUT_DIR: path.join(tempRoot, 'output'),
  },
})

const forwardSignal = (signal) => {
  if (!serve.killed) {
    serve.kill(signal)
  }
}
process.on('SIGINT', forwardSignal)
process.on('SIGTERM', forwardSignal)

serve.on('error', (err) => {
  console.error('failed to start server:', err)
  process.exit(1)
})

serve.on('exit', (code, signal) => {
  if (signal) {
    const map = { SIGINT: 130, SIGTERM: 143, SIGHUP: 129, SIGKILL: 137 }
    process.exit(map[signal] ?? 1)
  }
  process.exit(code ?? 1)
})
