import { useCallback, useMemo } from 'react';
import { useAIRuntimeCatalog } from '@/api/aiRuntime';
import type { ExecutorProfile } from '@/api/types';
import {
  buildRuntimeSelectorModel,
  runtimeModelChoicesForCLI,
  searchRuntimeCLIChoices,
  type RuntimeChoiceFilter,
  type RuntimeCLIChoice,
  type RuntimeModelChoice,
  type RuntimeSelectorModel,
} from './runtimeSelector';

export interface UseRuntimeSelectorInput {
  currentCLI?: string;
  currentModel?: string;
  currentExecutors?: ExecutorProfile[];
}

export interface RuntimeSelectorHook extends RuntimeSelectorModel {
  isLoading: boolean;
  isRefreshing: boolean;
  isError: boolean;
  errorMessage: string;
  refresh: () => void;
  cliChoicesForSearch: (search?: string) => RuntimeCLIChoice[];
  modelChoicesForCLI: (cli: string, currentModel?: string, filter?: RuntimeChoiceFilter) => RuntimeModelChoice[];
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
  const cliChoicesForSearch = useCallback(
    (search?: string) => searchRuntimeCLIChoices(model, search),
    [model],
  );
  const modelChoicesForCLI = useCallback(
    (cli: string, currentModel?: string, filter?: RuntimeChoiceFilter) =>
      runtimeModelChoicesForCLI(model, cli, currentModel, filter),
    [model],
  );

  return {
    ...model,
    isLoading: catalog.isLoading,
    isRefreshing: catalog.isFetching && !catalog.isLoading,
    isError: catalog.isError,
    errorMessage: catalog.error instanceof Error ? catalog.error.message : '',
    refresh: () => {
      void catalog.refetch();
    },
    cliChoicesForSearch,
    modelChoicesForCLI,
  };
}
