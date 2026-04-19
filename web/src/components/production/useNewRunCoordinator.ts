import { useContext } from 'react'
import { NewRunCoordinatorContext } from './newRunCoordinatorContext'

export function useNewRunCoordinator() {
  const value = useContext(NewRunCoordinatorContext)
  if (!value) {
    throw new Error(
      'useNewRunCoordinator must be used within a NewRunCoordinatorProvider',
    )
  }
  return value
}
