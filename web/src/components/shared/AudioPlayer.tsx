import { useEffect, useRef, useState } from 'react'
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'

interface AudioPlayerProps {
  duration_ms?: number | null
  scene_key: string | number
  src?: string | null
}

function formatTime(totalSeconds: number) {
  const safeSeconds = Math.max(0, Math.floor(totalSeconds))
  const minutes = Math.floor(safeSeconds / 60)
  const seconds = safeSeconds % 60
  return `${minutes}:${seconds.toString().padStart(2, '0')}`
}

export function AudioPlayer({ duration_ms, scene_key, src }: AudioPlayerProps) {
  const audio_ref = useRef<HTMLAudioElement | null>(null)
  const [current_time, set_current_time] = useState(0)
  const initial_duration = duration_ms != null ? duration_ms / 1000 : 0
  const [duration, set_duration] = useState(initial_duration)
  const [is_playing, set_is_playing] = useState(false)

  useEffect(() => {
    const audio = audio_ref.current
    if (!audio) {
      return
    }
    const media = audio

    function syncTime() {
      set_current_time(media.currentTime || 0)
    }

    function syncDuration() {
      if (Number.isFinite(media.duration) && media.duration > 0) {
        set_duration(media.duration)
      }
    }

    function onPlay() {
      set_is_playing(true)
    }

    function onPause() {
      set_is_playing(false)
    }

    function onEnded() {
      set_is_playing(false)
      set_current_time(0)
      media.currentTime = 0
    }

    media.addEventListener('timeupdate', syncTime)
    media.addEventListener('loadedmetadata', syncDuration)
    media.addEventListener('durationchange', syncDuration)
    media.addEventListener('play', onPlay)
    media.addEventListener('pause', onPause)
    media.addEventListener('ended', onEnded)

    return () => {
      media.removeEventListener('timeupdate', syncTime)
      media.removeEventListener('loadedmetadata', syncDuration)
      media.removeEventListener('durationchange', syncDuration)
      media.removeEventListener('play', onPlay)
      media.removeEventListener('pause', onPause)
      media.removeEventListener('ended', onEnded)
    }
  }, [scene_key])

  async function togglePlayback() {
    const audio = audio_ref.current
    if (!audio || !src) {
      return
    }

    if (is_playing) {
      audio.pause()
      set_is_playing(false)
      return
    }

    await audio.play()
    set_is_playing(true)
  }

  // Space must always be absorbed while the batch-review detail panel
  // is mounted — even when the scene has no audio — so the browser
  // does not scroll the page out from under the operator.
  useKeyboardShortcuts(
    [
      {
        action: 'audio-toggle',
        handler: () => {
          void togglePlayback()
        },
        key: 'space',
        prevent_default: true,
        scope: 'context',
      },
    ],
    { enabled: true },
  )

  if (!src) {
    return (
      <section className="audio-player audio-player--unavailable" aria-label="Narration audio">
        <p className="audio-player__eyebrow">Narration audio</p>
        <p className="audio-player__unavailable">Audio unavailable for this scene.</p>
      </section>
    )
  }

  const max = Math.max(duration, 0)

  return (
    <section className="audio-player" aria-label="Narration audio">
      <audio key={String(scene_key)} ref={audio_ref} preload="metadata" src={src} />
      <div className="audio-player__header">
        <div>
          <p className="audio-player__eyebrow">Narration audio</p>
          <p className="audio-player__hint">[Space] Play / pause</p>
        </div>
        <button
          type="button"
          className="audio-player__button"
          onClick={() => {
            void togglePlayback()
          }}
        >
          {is_playing ? 'Pause' : 'Play'}
        </button>
      </div>

      <div className="audio-player__timeline">
        <span>{formatTime(current_time)}</span>
        <input
          aria-label="Audio seekbar"
          className="audio-player__seekbar"
          max={max}
          min={0}
          onChange={(event) => {
            const nextTime = Number(event.currentTarget.value)
            if (audio_ref.current) {
              audio_ref.current.currentTime = nextTime
            }
            set_current_time(nextTime)
          }}
          step={0.1}
          type="range"
          value={Math.min(current_time, max)}
        />
        <span>{formatTime(max)}</span>
      </div>
    </section>
  )
}
