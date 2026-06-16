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

export type ReminderScheduleKind = 'once' | 'cron';

export interface ReminderSchedule {
  kind: ReminderScheduleKind;
  once_at?: string; // RFC3339 (once)
  cron_expr?: string; // (cron)
  timezone?: string; // IANA tz (cron)
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
  fired_count: number;
  version: number;
  schedule: ReminderSchedule;
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

// List filter — "全部" (all, owner view) vs "created" (mine). Status narrows.
export type ReminderListFilter = 'all' | 'created';

export interface ReminderListParams {
  filter?: ReminderListFilter;
  statuses?: ReminderStatus[];
}

function buildReminderQuery(p?: ReminderListParams): string {
  const q = new URLSearchParams();
  if (p?.filter) q.set('filter', p.filter);
  if (p?.statuses && p.statuses.length > 0) q.set('status', p.statuses.join(','));
  const s = q.toString();
  return s ? `?${s}` : '';
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GET /reminders → { reminders: Reminder[] }. slug only scopes the cache key/gate;
// the /orgs/{slug} segment is auto-injected by the client.
export function useReminders(slug: string | undefined, params?: ReminderListParams) {
  return useQuery({
    queryKey: qk.reminders({ slug, params }),
    queryFn: () =>
      api
        .get<{ reminders: Reminder[] }>(`/reminders${buildReminderQuery(params)}`)
        .then((r) => r.reminders),
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
  end_condition?: ReminderEndCondition;
}

export function useCreateReminder(slug: string | undefined) {
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

export function useUpdateReminder(slug: string | undefined) {
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
