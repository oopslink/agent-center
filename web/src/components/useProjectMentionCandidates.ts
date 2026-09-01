import { useMemo } from 'react';
import { identityRefOf, normalizeIdentityRef, refKind, useMembers, type MemberResult } from '@/api/members';
import { useProjectMembers } from '@/api/projects';
import type { ProjectMember } from '@/api/types';
import type { MentionOption } from './MentionPicker';

function memberRef(member: MemberResult): string {
  return identityRefOf({ kind: member.kind, identity_id: member.identity_id });
}

function projectMemberRef(member: ProjectMember): string {
  return identityRefOf({ kind: refKind(member.identity_id), identity_id: member.identity_id });
}

export function useProjectMentionCandidates(projectId: string | undefined): MentionOption[] | undefined {
  const members = useMembers();
  const projectMembers = useProjectMembers(projectId);

  return useMemo(() => {
    if (!projectId) return undefined;
    const directory = new Map<string, MemberResult>();
    for (const member of members.data ?? []) {
      directory.set(memberRef(member), member);
    }
    return (projectMembers.data ?? [])
      .map((projectMember) => {
        const ref = projectMemberRef(projectMember);
        const directoryMember = directory.get(ref);
        return {
          id: ref,
          name: directoryMember?.display_name ?? normalizeIdentityRef(ref),
          secondary: ref,
        };
      })
      .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
  }, [members.data, projectId, projectMembers.data]);
}
