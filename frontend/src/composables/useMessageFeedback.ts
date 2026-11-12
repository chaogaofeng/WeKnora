import { reactive } from 'vue'

export interface MessageFeedbackItem {
  id: string
  message_id: string
  from_source: 'user' | 'admin'
  rating: number
  content: string
}

export interface FeedbackApi {
  list: (sessionId: string) => Promise<MessageFeedbackItem[]>
  save: (
    sessionId: string,
    payload: { message_id: string; from_source: 'user' | 'admin'; rating: number; content: string },
  ) => Promise<MessageFeedbackItem>
}

interface SessionFeedbackCache {
  loaded: boolean
  loading: boolean
  // message_id -> user feedback for this session
  byMessage: Map<string, MessageFeedbackItem>
}

// Shared cache across every message component mounted for the same session, so a
// session is only fetched once even when many assistant messages are rendered.
const store = new Map<string, SessionFeedbackCache>()

function getCache(sessionId: string): SessionFeedbackCache {
  let cache = store.get(sessionId)
  if (!cache) {
    cache = reactive({
      loaded: false,
      loading: false,
      byMessage: new Map<string, MessageFeedbackItem>(),
    })
    store.set(sessionId, cache)
  }
  return cache
}

/**
 * Reactive per-session message feedback (thumbs up / down) store.
 *
 * `api.list` returns the raw feedback rows for the session; `api.save` upserts a
 * single row and resolves with the persisted entity. Only `user`-source ratings
 * are kept in the reactive view — admin annotations are a separate console
 * surface and are not shown to end users.
 */
export function useMessageFeedback(sessionId: string, api: FeedbackApi) {
  const cache = getCache(sessionId)

  async function ensureLoaded(): Promise<void> {
    if (cache.loaded || cache.loading) return
    cache.loading = true
    try {
      const list = await api.list(sessionId)
      cache.byMessage.clear()
      for (const f of list) {
        if (f.from_source === 'user') cache.byMessage.set(f.message_id, f)
      }
      cache.loaded = true
    } catch {
      // Feedback is best-effort; a failed load simply leaves the UI un-seeded.
    } finally {
      cache.loading = false
    }
  }

  function getRating(messageId: string): number {
    return cache.byMessage.get(messageId)?.rating ?? 0
  }

  /**
   * Toggle or switch the rating for a message. Clicking the active rating again
   * clears it (rating 0); clicking the opposite rating switches to it.
   * `content` (optional) lets the caller attach a free-text note in the same upsert.
   */
  async function rate(messageId: string, rating: number, content = ''): Promise<number> {
    const current = cache.byMessage.get(messageId)?.rating ?? 0
    const next = current === rating ? 0 : rating
    const saved = await api.save(sessionId, {
      message_id: messageId,
      from_source: 'user',
      rating: next,
      content,
    })
    if (next === 0) {
      cache.byMessage.delete(messageId)
    } else {
      cache.byMessage.set(messageId, saved)
    }
    return next
  }

  /** Attach / replace the free-text note on an already-rated message. */
  async function setContent(messageId: string, content: string): Promise<void> {
    const current = cache.byMessage.get(messageId)?.rating ?? 0
    if (current === 0) return
    const saved = await api.save(sessionId, {
      message_id: messageId,
      from_source: 'user',
      rating: current,
      content,
    })
    cache.byMessage.set(messageId, saved)
  }

  return { cache, ensureLoaded, getRating, rate, setContent }
}
