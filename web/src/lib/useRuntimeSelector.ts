import { useMemo } from 'react';
import { useAIRuntimeCatalog } from '@/api/aiRuntime';
import type { ExecutorProfile } from '@/api/types';
import {
  buildRuntimeSelectorModel,
  type RuntimeSelectorModel,
} from './runtimeSelector';

export interface UseRuntimeSelectorInput {
  currentCLI?: string;
  currentModel?: string;
  currentExecutors?: ExecutorProfile[];
}

export interface RuntimeSelectorHook extends RuntimeSelectorModel {
  isLoading: boolean;
  isError: boolean;
  errorMessage: string;
  refresh: () => void;
}

export function useRuntimeSelector(input: UseRuntimeSelectorInput = {}): RuntimeSelectorHook {
  const catalog = useAIRuntimeCatalog();
  const model = useMemo(
    () => buildRuntimeSelectorModel(catalog.data, {
      cli: input.currentCLI,
      model: input.currentModel,
      executors: input.currentExecutors,
    }),
    [catalog.data, input.currentCLI, input.currentModel, input.currentExecutors],
  );

  return {
    ...model,
    isLoading: catalog.isLoading,
    isError: catalog.isError,
    errorMessage: catalog.error instanceof Error ? catalog.error.message : '',
    refresh: () => {
      void catalog.refetch();
    },
  };
}
