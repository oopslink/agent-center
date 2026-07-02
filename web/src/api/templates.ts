import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Template } from './types';

export function useTemplates() {
  return useQuery({
    queryKey: qk.templates(),
    queryFn: async () => {
      const resp = await api.get<{ templates: Template[] }>('/templates');
      return resp.templates;
    },
  });
}

export function useTemplate(id: string) {
  return useQuery({
    queryKey: qk.template(id),
    queryFn: async () => {
      return api.get<Template>(`/templates/${id}`);
    },
    enabled: !!id,
  });
}

export function useCreateTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (data: { name: string; description: string; content: string }) => {
      return api.post<Template>('/templates', data);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.templates() }),
  });
}

export function useUpdateTemplate(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (data: { name: string; description: string; content: string }) => {
      return api.put<Template>(`/templates/${id}`, data);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.templates() });
      qc.invalidateQueries({ queryKey: qk.template(id) });
    },
  });
}

export function useDeleteTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      return api.del(`/templates/${id}`);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.templates() }),
  });
}
