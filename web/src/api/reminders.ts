import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// Reminders — T207 (Agent 提醒 / 定时, Cognition BC). The human web-console CRUD
// over /api/orgs/{slug}/reminders (the /orgs/{slug} segment is auto-injected by
// the client). Mirrors the locked T206 backend contract (schedule once|cron,
// status active|paused|completed|canceled, end_condition never|until|max_count).

// ---------------------------------------------------------------------------
// Contract types
// ---------------------------------------------------------------------------

export type ReminderStatus = 'active' | 'paused' | 'completed' | 'canceled';

export type ReminderScheduleKind = 'once' | 'cron' | 'on_event';

export interface ReminderSchedule {
  kind: ReminderScheduleKind;
  once_at?: string; // RFC3339 (once)
  cron_expr?: string; // (cron)
  timezone?: string; // IANA tz (cron)
}

// ReminderOnEvent is the event-driven trigger spec (kind === 'on_event'): the
// reminder stays dormant until the named entity state-change event fires, then
// arms (+delay) and fires once. Emitted alongside schedule by the API.
export interface ReminderOnEvent {
  entity_type: string; // plan | task | issue
  entity_id: string;
  event: string; // e.g. completed | failed | blocked | closed
  delay_seconds: number;
}

export type ReminderEndKind = 'never' | 'until' | 'max_count';

export interface ReminderEndCondition {
  kind: ReminderEndKind;
  until?: string; // RFC3339 (until)
  max_count?: number; // (max_count)
}

export interface Reminder {
  id: string;
  organization_id: string;
  project_id: string;
  creator_ref: string; // user:<id> | agent:<id>
  remindee_agent_id: string;
  content: string;
  status: ReminderStatus;
  skip_if_overlap: boolean;
  deliver_as_creator: boolean;
  fired_count: number;
  version: number;
  schedule: ReminderSchedule;
  on_event?: ReminderOnEvent; // present when schedule.kind === 'on_event'
  next_run_at?: string | null;
  last_fired_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ReminderFiring {
  id: string;
  reminder_id: string;
  fired_at: string;
  outcome: 'delivered' | 'skipped_overlap' | 'failed';
  detail: string;
}

export interface ReminderDetail extends Reminder {
  firings: ReminderFiring[];
}

// List filter — "全部" (all, owner view) / "created" (我创建的) / "remindee"
// (提醒我的 — reminders targeting the current viewing identity). Status narrows.
export type ReminderListFilter = 'all' | 'created' | 'remindee';

export interface ReminderListParams {
  filter?: ReminderListFilter;
  statuses?: ReminderStatus[];
  /** server-side content search (contains, case-insensitive). */
  q?: string;
  /** sort column key: created_at | updated_at | status | next_run_at. */
  sort?: string;
  dir?: 'asc' | 'desc';
  /** 1-based page (with page_size). */
  page?: number;
  page_size?: number;
}

function buildReminderQuery(p?: ReminderListParams): string {
  const q = new URLSearchParams();
  if (p?.filter) q.set('filter', p.filter);
  if (p?.statuses && p.statuses.length > 0) q.set('status', p.statuses.join(','));
  if (p?.q) q.set('q', p.q);
  if (p?.sort) q.set('sort', p.sort);
  if (p?.dir) q.set('dir', p.dir);
  if (p?.page && p.page > 1) q.set('page', String(p.page));
  if (p?.page_size) q.set('page_size', String(p.page_size));
  const s = q.toString();
  return s ? `?${s}` : '';
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GET /reminders → { reminders: Reminder[], total }. Returns { items, total } so
// the list can render server-side pagination. slug only scopes the cache key/gate;
// the /orgs/{slug} segment is auto-injected by the client.
export function useReminders(slug: string | undefined, params?: ReminderListParams) {
  return useQuery({
    queryKey: qk.reminders({ slug, params }),
    queryFn: () =>
      api
        .get<{ reminders: Reminder[]; total?: number }>(`/reminders${buildReminderQuery(params)}`)
        .then((r) => ({ items: r.reminders ?? [], total: r.total ?? (r.reminders ?? []).length })),
    enabled: !!slug,
  });
}

// GET /reminders/{id} → Reminder + firings (历史触发).
export function useReminder(slug: string | undefined, id: string | undefined) {
  return useQuery({
    queryKey: qk.reminder(id ?? ''),
    queryFn: () => api.get<ReminderDetail>(`/reminders/${id}`),
    enabled: !!slug && !!id,
  });
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

export interface CreateReminderInput {
  remindee_agent_id: string;
  schedule: ReminderSchedule;
  content: string;
  skip_if_overlap?: boolean;
  deliver_as_creator?: boolean; // F-B: deliver as creator identity vs system (default ON)
  end_condition?: ReminderEndCondition;
}

export function useCreateReminder() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateReminderInput) => api.post<Reminder>('/reminders', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.reminders() });
    },
  });
}

export type ReminderAction = 'pause' | 'resume' | 'cancel' | 'edit';

export interface UpdateReminderInput {
  id: string;
  action: ReminderAction;
  schedule?: ReminderSchedule; // edit only
  content?: string; // edit only
}

export function useUpdateReminder() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateReminderInput) =>
      api.patch<Reminder>(`/reminders/${id}`, body),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: qk.reminders() });
      void qc.invalidateQueries({ queryKey: qk.reminder(r.id) });
    },
  });
}

// useDeleteReminder — hard-delete a reminder entry (T477). DELETE /reminders/{id}
// (204). Distinct from the 'cancel' action (a terminal status that keeps the row
// + firing history): delete removes the entry entirely. Invalidates the list.
export function useDeleteReminder() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/reminders/${id}`),
    onSuccess: (_data, id) => {
      void qc.invalidateQueries({ queryKey: qk.reminders() });
      void qc.invalidateQueries({ queryKey: qk.reminder(id) });
    },
  });
}
