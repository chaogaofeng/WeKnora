import { get, post, del } from '@/utils/request'

// SessionListItem is one row in the tenant-wide session list. IM origin fields
// are populated from im_channel_sessions when the session came from an IM platform.
export interface SessionListItem {
  id: string
  title: string
  description: string
  tenant_id: number
  kind: string
  user_id?: string
  owner_auth_source?: string
  is_pinned: boolean
  created_at: string
  updated_at: string
  agent_id?: string
  im_platform?: string
  im_chat_id?: string
  im_thread_id?: string
  im_user_id?: string
  im_agent_id?: string
  im_channel_id?: string
  owner_name?: string
  message_count?: number
  user_feedback_up?: number
  user_feedback_down?: number
  admin_feedback_count?: number
}

// MessageFeedback is a per-message rating/annotation (user feedback or admin note).
export interface MessageFeedback {
  id: string
  tenant_id: number
  session_id: string
  message_id: string
  from_source: 'user' | 'admin'
  rating: number
  content: string
  creator_id: string
  created_at: string
  updated_at: string
}

// listSessionFeedbacks returns every feedback row for a session.
export function listSessionFeedbacks(sessionId: string) {
  return get<{ success: boolean; data: MessageFeedback[] }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/feedbacks`,
  )
}

// saveSessionFeedback upserts a per-message feedback (from_source: 'user' | 'admin').
export function saveSessionFeedback(
  sessionId: string,
  payload: { message_id: string; from_source: 'user' | 'admin'; rating: number; content: string },
) {
  return post<{ success: boolean; data: MessageFeedback }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/feedbacks`,
    payload,
  )
}

// deleteSessionFeedback removes a feedback row by id.
export function deleteSessionFeedback(sessionId: string, feedbackId: string) {
  return del<{ success: boolean }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/feedbacks/${encodeURIComponent(feedbackId)}`,
  )
}

export interface SessionListResult {
  success: boolean
  data: SessionListItem[]
  total: number
  page: number
  page_size: number
}

export interface SessionListParams {
  kind?: string
  keyword?: string
  source?: string
  agent_id?: string
  page?: number
  page_size?: number
}

// listSessions queries the tenant-wide session list. Source values:
//   '' (all) / 'web' / 'embed' / 'api' / 'feishu' / 'wecom' / 'embed:<channelID>'.
// embed/api/IM sources require Admin+ (enforced server-side).
export function listSessions(params: SessionListParams) {
  const query = new URLSearchParams()
  if (params.kind) query.set('kind', params.kind)
  if (params.keyword) query.set('keyword', params.keyword)
  if (params.source) query.set('source', params.source)
  if (params.agent_id) query.set('agent_id', params.agent_id)
  if (params.page) query.set('page', String(params.page))
  if (params.page_size) query.set('page_size', String(params.page_size))
  const qs = query.toString()
  return get<SessionListResult>(`/api/v1/sessions${qs ? `?${qs}` : ''}`)
}

export interface ChatMessage {
  id: string
  session_id: string
  request_id: string
  role: string
  content: string
  created_at: string
  knowledge_references?: unknown[]
}

// loadSessionMessages returns the full transcript of one conversation.
export function loadSessionMessages(sessionId: string) {
  return get<{ success: boolean; data: ChatMessage[] }>(
    `/api/v1/messages/${encodeURIComponent(sessionId)}/load`,
  )
}
