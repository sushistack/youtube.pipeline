import { mkdir } from 'node:fs/promises'
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

await mkdir(path.join(tempRoot, 'output'), { recursive: true })

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
