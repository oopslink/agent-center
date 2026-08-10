import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useAddMember, useAddAgentMember } from '@/api/members';
import { useFleet } from '@/api/fleet';
import { useAIRuntimeCatalog } from '@/api/aiRuntime';
import { ApiError } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';
import { EntitySelect } from '@/components/EntitySelect';
import { deriveRuntimeChoices, validateRuntimePair } from '@/components/executorProfiles';
import { useTranslation } from 'react-i18next';

// MemberNew backs /organizations/{slug}/members/new?kind=agent|user.
// Acceptance plan §3 references /members/new?kind=agent as the Add Agent entry.
export default function MemberNew(): React.ReactElement {
  const { t } = useTranslation('members');
  const [params] = useSearchParams();
  const kind = params.get('kind') === 'user' ? 'user' : 'agent';
  const navigate = useNavigate();
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';

  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [role, setRole] = useState('member');
  // v2.7 #157: Members→Add Agent is one step — also create the execution Agent
  // (model/cli + the worker it runs on). worker_id is required for an agent.
  const [model, setModel] = useState('');
  const [cli, setCli] = useState('');
  const [workerID, setWorkerID] = useState('');
  const [error, setError] = useState('');
  const [tempPasscode, setTempPasscode] = useState('');

  const addUser = useAddMember();
  const addAgent = useAddAgentMember();
  const fleet = useFleet();
  const runtimeCatalog = useAIRuntimeCatalog();
  const pending = addUser.isPending || addAgent.isPending;
  const workers = fleet.data?.workers ?? [];
  const selectedWorker = workers.find((w) => w.worker_id === workerID);
  const runtimeChoices = useMemo(
    () => deriveRuntimeChoices(runtimeCatalog.data, selectedWorker),
    [runtimeCatalog.data, selectedWorker],
  );
  const validationMessages = {
    catalogLoading: t('agents.create.validation.catalogLoading'),
    catalogMissing: t('agents.create.validation.catalogMissing'),
    workerMissing: t('agents.create.validation.workerMissing'),
    cliUnavailable: (value: string) => t('agents.create.validation.cliUnavailable', { cli: value }),
    modelUnavailable: (value: string, cliValue: string) =>
      t('agents.create.validation.modelUnavailable', { model: value, cli: cliValue }),
    executorUnavailable: (cliValue: string, modelValue: string) =>
      t('agents.create.validation.executorUnavailable', { cli: cliValue, model: modelValue }),
  };
  const runtimeError = validateRuntimePair(runtimeChoices, cli, model, validationMessages, {
    catalogLoading: runtimeCatalog.isLoading,
    catalogReady: runtimeCatalog.isSuccess,
    workerSelected: !!workerID,
  });

  useEffect(() => {
    if (!workerID || !runtimeChoices.defaultCli) return;
    setCli((current) =>
      runtimeChoices.cliOptions.some((option) => option.value === current)
        ? current
        : runtimeChoices.defaultCli,
    );
  }, [runtimeChoices, workerID]);

  useEffect(() => {
    if (!workerID || !cli) return;
    const modelOptions = runtimeChoices.modelOptionsByCli[cli] ?? [];
    const fallbackModel =
      cli === runtimeChoices.defaultCli ? runtimeChoices.defaultModel : (modelOptions[0]?.value ?? '');
    setModel((current) =>
      modelOptions.some((option) => option.value === current)
        ? current
        : fallbackModel,
    );
  }, [cli, runtimeChoices, workerID]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (kind === 'agent') {
      const submitError = validateRuntimePair(runtimeChoices, cli, model, validationMessages, {
        catalogLoading: runtimeCatalog.isLoading,
        catalogReady: runtimeCatalog.isSuccess,
        workerSelected: !!workerID,
      });
      if (submitError) {
        setError(submitError);
        return;
      }
      addAgent.mutate(
        {
          display_name: displayName.trim(),
          description: description.trim(),
          role,
          model: model.trim(),
          cli,
          worker_id: workerID,
        },
        {
          // v2.7 #185/#77: business-layer agent id = member identity_id (entity
          // id is internal-only). Navigate to AgentDetail by identity_id
          // (GET /api/agents/{identity_id} resolves via the member→entity bridge).
          onSuccess: (res) =>
            // dev2/v281: fall back to the canonical /agents list (the retired
            // /members/agents now just redirects there).
            navigate(res.identity_id ? `${base}/agents/${res.identity_id}` : `${base}/agents`),
          onError: (err) => setError(err instanceof ApiError ? err.message : t('humans.new.createFailed')),
        },
      );
    } else {
      addUser.mutate(
        { display_name: displayName.trim(), role },
        {
          onSuccess: (res) => {
            if (res.temp_passcode) setTempPasscode(res.temp_passcode);
            else navigate(`${base}/members/humans`);
          },
          onError: (err) => setError(err instanceof ApiError ? err.message : t('humans.new.createFailed')),
        },
      );
    }
  };

  if (tempPasscode) {
    return (
      <section className="space-y-4 max-w-md" data-testid="page-MemberNew">
        <h2 className="text-xl font-semibold text-text-primary">{t('humans.new.userCreated')}</h2>
        <p className="text-sm text-text-secondary">{t('humans.new.tempPasscodeHint')}</p>
        <div className="rounded bg-bg-subtle border border-border-strong px-3 py-3 text-center">
          <code className="text-2xl font-mono tracking-widest text-text-primary">{tempPasscode}</code>
        </div>
        <button
          type="button"
          onClick={() => navigate(`${base}/members/humans`)}
          className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          {t('humans.new.savedBackToMembers')}
        </button>
      </section>
    );
  }

  return (
    <section className="space-y-4 max-w-md" data-testid="page-MemberNew">
      <h2 className="text-xl font-semibold text-text-primary">
        {kind === 'agent' ? t('humans.new.addAgent') : t('humans.new.addUser')}
      </h2>
      {error && (
        <div role="alert" className="rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}
      <form onSubmit={handleSubmit} noValidate className="space-y-3 bg-bg-elevated border border-border rounded-lg p-4">
        <div className="space-y-1">
          <label htmlFor="mn-name" className="block text-sm text-text-primary">{t('humans.new.displayName')}</label>
          <input
            id="mn-name"
            type="text"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
            placeholder={kind === 'agent' ? t('humans.new.agentNamePlaceholder') : t('humans.new.userNamePlaceholder')}
          />
        </div>
        {kind === 'agent' && (
          <div className="space-y-1">
            <label htmlFor="mn-desc" className="block text-sm text-text-primary">{t('humans.new.descriptionOptional')}</label>
            <input
              id="mn-desc"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
            />
          </div>
        )}
        {kind === 'agent' && (
          <>
            {/* v2.7 #157: execution-agent fields — one-step create runs the agent on a worker. */}
            <div className="space-y-1">
              <span className="block text-sm text-text-primary">{t('humans.new.runOnWorker')}</span>
              {/* v2.7 #191: shared searchable EntitySelect instead of a raw <select>. */}
              <EntitySelect
                testId="mn-worker"
                value={workerID}
                onChange={(next) => {
                  setWorkerID(next);
                  setError('');
                }}
                options={workers.map((w) => ({
                  value: w.worker_id,
                  label: w.name || w.worker_id,
                  badge: w.status,
                }))}
                placeholder={t('humans.new.workerPlaceholder')}
                searchPlaceholder={t('humans.new.workerSearchPlaceholder')}
                ariaLabel={t('humans.new.runOnWorker')}
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="mn-model" className="block text-sm text-text-primary">{t('humans.new.modelOptional')}</label>
              <select
                id="mn-model"
                value={model}
                onChange={(e) => {
                  setModel(e.target.value);
                  setError('');
                }}
                disabled={!workerID || !cli || (runtimeChoices.modelOptionsByCli[cli] ?? []).length === 0}
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              >
                {(runtimeChoices.modelOptionsByCli[cli] ?? []).map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1">
              <label htmlFor="mn-cli" className="block text-sm text-text-primary">{t('humans.new.cli')}</label>
              <select
                id="mn-cli"
                value={cli}
                onChange={(e) => {
                  setCli(e.target.value);
                  setError('');
                }}
                disabled={!workerID || runtimeChoices.cliOptions.length === 0}
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              >
                {runtimeChoices.cliOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              <p className="text-xs text-text-muted">{t('humans.new.cliHint')}</p>
            </div>
          </>
        )}
        <div className="space-y-1">
          <label htmlFor="mn-role" className="block text-sm text-text-primary">{t('humans.new.role')}</label>
          <select
            id="mn-role"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary"
          >
            <option value="member">{t('humans.role.member')}</option>
            <option value="admin">{t('humans.role.admin')}</option>
            {kind === 'user' && <option value="owner">{t('humans.role.owner')}</option>}
          </select>
        </div>
        <div className="flex gap-2 justify-end">
          <button
            type="button"
            onClick={() => navigate(kind === 'agent' ? `${base}/agents` : `${base}/members/humans`)}
            className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
          >
            {t('humans.new.cancel')}
          </button>
          <button
            type="submit"
            disabled={pending || !displayName.trim() || (kind === 'agent' && !!runtimeError)}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          >
            {pending ? t('humans.new.creating') : t('humans.new.create')}
          </button>
        </div>
      </form>
    </section>
  );
}
