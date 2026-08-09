import type React from 'react';
import { useOptionalOrgContext, orgPath } from '@/OrgContext';
import {
  renderEntityRefParts,
  tokenizeEntityRefs,
  type EntityRefSurface,
  type EntityRefToken,
  type EntityRefVariant,
  type ResolvedEntityRef,
} from './entityRefCore';
import {
  useAgentRefResolver,
  useIssueRefResolver,
  usePlanRefResolver,
  useTaskRefResolver,
} from './MentionText';

type AgentRefMode = 'link' | 'sidebar' | 'plain';

export interface EntityRefTextProps {
  text: string;
  className?: string;
  variant?: EntityRefVariant;
  surface?: EntityRefSurface;
  linkClass?: string;
  agentMode?: AgentRefMode;
  onAgentRef?: (ref: string) => void;
}

export function EntityRefText({
  text,
  className,
  variant = 'label',
  surface = 'generic',
  linkClass = 'text-accent',
  agentMode = 'link',
  onAgentRef,
}: EntityRefTextProps): React.ReactElement {
  const ctx = useOptionalOrgContext();
  const slug = ctx?.slug;
  const resolveTask = useTaskRefResolver();
  const resolvePlan = usePlanRefResolver();
  const resolveIssue = useIssueRefResolver();
  const resolveAgent = useAgentRefResolver();

  const resolve = (token: EntityRefToken): ResolvedEntityRef | null => {
    if (token.kind === 'task') {
      const task = resolveTask(token.token);
      if (!task) return null;
      return {
        kind: 'task',
        token: token.token,
        label: task.label,
        href: task.href,
        dataAttrs: { 'data-task-id': token.token },
      };
    }
    if (token.kind === 'plan') {
      const plan = resolvePlan(token.token);
      if (!plan) return null;
      return {
        kind: 'plan',
        token: token.token,
        label: plan.label,
        href: plan.href,
        dataAttrs: { 'data-plan-id': token.token },
      };
    }
    if (token.kind === 'issue') {
      const issue = resolveIssue(token.token);
      if (!issue) return null;
      return {
        kind: 'issue',
        token: token.token,
        label: issue.label,
        href: issue.href,
        dataAttrs: { 'data-issue-id': token.token },
      };
    }
    if (token.kind === 'agent') {
      const agent = resolveAgent(token.token);
      if (!agent || !agent.ref.startsWith('agent:')) return null;
      if (agentMode === 'plain') return null;
      return {
        kind: 'agent',
        token: token.token,
        label: agent.label,
        href: agentMode === 'link' ? orgPath(`/agents/${encodeURIComponent(token.token)}`, slug) : undefined,
        onClick: agentMode === 'sidebar' && onAgentRef ? () => onAgentRef(agent.ref) : undefined,
        dataAttrs: { 'data-agent-ref': agent.ref },
      };
    }
    // There is no front-end executor/run detail route today. Recognize the token
    // centrally, but degrade to source text until a resolver can produce context.
    return null;
  };

  const parts = renderEntityRefParts({
    text,
    tokens: tokenizeEntityRefs(text),
    resolve,
    variant,
    surface,
    linkClass,
  });
  return <span className={className}>{parts}</span>;
}
