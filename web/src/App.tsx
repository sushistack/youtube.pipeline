import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { ProductionShell } from './components/shells/ProductionShell'
import { SettingsShell } from './components/shells/SettingsShell'
import { TuningShell } from './components/shells/TuningShell'
import { AppShell } from './components/shared/AppShell'
import { KeyboardShortcutsProvider } from './hooks/useKeyboardShortcuts'

function App() {
  return (
    <KeyboardShortcutsProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route index element={<Navigate to="/production" replace />} />
            <Route path="production" element={<ProductionShell />} />
            <Route path="tuning" element={<TuningShell />} />
            <Route path="settings" element={<SettingsShell />} />
            <Route path="*" element={<Navigate to="/production" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </KeyboardShortcutsProvider>
  )
}

export default App
